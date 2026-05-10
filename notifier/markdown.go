package main

import (
	"bytes"
	"context"
	"embed"
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
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
)

//go:embed assets
var hljsAssets embed.FS

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

// RenderMarkdownToHTML 将 Markdown 转为 HTML，使用 Chroma 内联样式高亮。
// 适用于邮件正文等不支持 JavaScript 的场景。
func RenderMarkdownToHTML(markdown string) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(false), // 使用 inline style，兼容邮件客户端
				),
			),
		),
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

// renderMarkdownPlain 将 Markdown 转为 HTML，保留原始 <code> 标签不做高亮。
// 交由 highlight.js 在浏览器端完成高亮，效果更精细。
func renderMarkdownPlain(markdown string) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
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

// buildHighlightScripts 拼接所有 highlight.js 资源（核心 + 语言包）为一段 <script>
func buildHighlightScripts() string {
	var sb strings.Builder

	// 核心 JS
	if data, err := hljsAssets.ReadFile("assets/highlight.min.js"); err == nil {
		sb.WriteString("<script>")
		sb.Write(data)
		sb.WriteString("</script>\n")
	}

	// 语言包
	entries, _ := hljsAssets.ReadDir("assets")
	for _, e := range entries {
		name := e.Name()
		if name == "highlight.min.js" || name == "github.min.css" {
			continue
		}
		if strings.HasSuffix(name, ".min.js") {
			if data, err := hljsAssets.ReadFile("assets/" + name); err == nil {
				sb.WriteString("<script>")
				sb.Write(data)
				sb.WriteString("</script>\n")
			}
		}
	}

	// 触发高亮
	sb.WriteString("<script>hljs.highlightAll();</script>\n")
	return sb.String()
}

func GeneratePDF(markdownContent string, taskID string) (string, error) {
	// 使用不带高亮的 HTML，交由 highlight.js 浏览器端渲染
	htmlBody, err := renderMarkdownPlain(markdownContent)
	if err != nil {
		return "", err
	}

	// 读取 highlight.js CSS 主题
	hljsCSS := ""
	if data, err := hljsAssets.ReadFile("assets/github.min.css"); err == nil {
		hljsCSS = string(data)
	}

	// 拼接 highlight.js 脚本
	hljsScripts := buildHighlightScripts()

	fullHTML := fmt.Sprintf(`
	<!DOCTYPE html>
	<html>
	<head>
		<meta charset="utf-8">
		<style>
			%s
			body { font-family: "Microsoft YaHei", sans-serif; padding: 20px; line-height: 1.6; color: #333; }
			table { border-collapse: collapse; width: 100%%; }
			table, th, td { border: 1px solid #ddd; padding: 8px; }
			th { background-color: #f2f2f2; }
			code { background-color: #f4f4f4; padding: 2px 4px; border-radius: 4px; }
			pre { background-color: #f6f8fa; padding: 16px; border-radius: 6px; overflow-x: auto; border: 1px solid #e1e4e8; }
			pre code { background-color: transparent; padding: 0; }
		</style>
	</head>
	<body>
		%s
		%s
	</body>
	</html>
	`, hljsCSS, htmlBody, hljsScripts)

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
		// 等待 highlight.js 完成渲染
		chromedp.WaitReady("body"),
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
