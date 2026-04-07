package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func ExtractSummary(markdown string) string {
	// Match "## ...概要" headings, allowing for:
	//   - Chinese ordinal prefixes like "一、" "二、" or numeric "1." "2." etc.
	//   - Extra whitespace between ## and the title text
	//   - Arbitrary prefix text before "概要", e.g. "代码检视结果概要"
	// Captures from the heading line through to the next ## heading or EOF.
	summaryRegex := regexp.MustCompile(`(?im)(^##\s+(?:[一二三四五六七八九十\d]+[、.]\s*)?.*概要.*\n[\s\S]*?)(?:^##\s|\z)`)
	matches := summaryRegex.FindStringSubmatch(markdown)

	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]) + "\n\n"
	}

	// Fallback: try matching "概述" if "概要" was not found
	overviewRegex := regexp.MustCompile(`(?im)(^##\s+(?:[一二三四五六七八九十\d]+[、.]\s*)?.*概述.*\n[\s\S]*?)(?:^##\s|\z)`)
	matches = overviewRegex.FindStringSubmatch(markdown)

	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]) + "\n\n"
	}

	return "具体报告，请查阅随附的完整版附件。\n\n"
}

func RenderMarkdownToHTML(markdown string) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func GeneratePDF(markdownContent string, taskID string) (string, error) {
	htmlBody, err := RenderMarkdownToHTML(markdownContent)
	if err != nil {
		return "", err
	}

	fullHTML := fmt.Sprintf(`
	<!DOCTYPE html>
	<html>
	<head>
		<meta charset="utf-8">
		<style>
			body { font-family: "Microsoft YaHei", sans-serif; padding: 20px; line-height: 1.6; color: #333; }
			table { border-collapse: collapse; width: 100%%; }
			table, th, td { border: 1px solid #ddd; padding: 8px; }
			th { background-color: #f2f2f2; }
			code { background-color: #f4f4f4; padding: 2px 4px; border-radius: 4px; }
			pre { background-color: #f4f4f4; padding: 10px; border-radius: 4px; overflow-x: auto; }
		</style>
	</head>
	<body>
		%s
	</body>
	</html>
	`, htmlBody)

	tempDir := filepath.Join(os.TempDir(), "code-shield-notifier")
	os.MkdirAll(tempDir, 0755)

	if taskID == "" {
		taskID = "default"
	}
	htmlPath := filepath.Join(tempDir, fmt.Sprintf("temp-%s.html", taskID))
	err = os.WriteFile(htmlPath, []byte(fullHTML), 0644)
	if err != nil {
		return "", err
	}
	defer os.Remove(htmlPath)

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	var pdfBuffer []byte
	fileURL := "file:///" + filepath.ToSlash(htmlPath)

	err = chromedp.Run(ctx,
		chromedp.Navigate(fileURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuffer, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return "", err
	}

	pdfPath := filepath.Join(tempDir, fmt.Sprintf("report-%s.pdf", taskID))
	err = os.WriteFile(pdfPath, pdfBuffer, 0644)
	if err != nil {
		return "", err
	}

	return pdfPath, nil
}
