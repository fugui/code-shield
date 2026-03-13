const express = require('express');
const bodyParser = require('body-parser');
const { marked } = require('marked');
const HTMLtoDOCX = require('html-to-docx');
const fs = require('fs');
const path = require('path');
const { exec } = require('child_process');
const os = require('os');

const app = express();
app.use(bodyParser.json({ limit: '50mb' }));

app.post('/api/notify/email', async (req, res) => {
    console.log(`[Notifier] Received POST webhook`);
    const { task_id, repo_name, branch, recipients, subject, markdown_content } = req.body;
    
    if (!markdown_content) {
        return res.status(400).json({ success: false, error: 'Missing markdown_content payload' });
    }

    let summaryText = "";
    try {
        // Safe regex extraction for heading sections
        const overviewMatch = markdown_content.match(/# 1\. 概述[\s\S]*?(?=# 2\.)/i);
        // Safely extract chapter 3 to end
        const conclusionMatch = markdown_content.match(/# 3\. (?:\S+)?总结[\s\S]*/i) || markdown_content.match(/# 3\. 代码检视总结[\s\S]*/i);
        
        if (overviewMatch) summaryText += overviewMatch[0] + "\n\n";
        if (conclusionMatch) summaryText += conclusionMatch[0] + "\n\n";
        
        if (!summaryText) {
             console.log(`[Notifier] Regex matching failed or sections missing, falling back to basic preview.`);
             summaryText = "无法截取固定段落，请查阅随附的 Word 完整报告附件。";
        }
    } catch(e) {
        summaryText = "摘要截取失败。 " + e;
    }

    // Convert text summary to rich HTML
    const htmlBody = marked.parse(summaryText);
    
    // Parse the entire document for Docx formatting
    const fullHtmlForDocx = `<article>${marked.parse(markdown_content)}</article>`;
    
    const tempDir = path.join(__dirname, '../temp');
    if (!fs.existsSync(tempDir)) {
        fs.mkdirSync(tempDir);
    }
    const docxName = `report-${task_id || Date.now()}.docx`;
    const docxPath = path.join(tempDir, docxName);
    
    try {
        const fileBuffer = await HTMLtoDOCX(fullHtmlForDocx, null, {
            table: { row: { cantSplit: true } },
            footer: true,
            pageNumber: true
        });
        fs.writeFileSync(docxPath, fileBuffer);
        console.log(`[Notifier] Rendered Docx report to: ${docxPath}`);
    } catch (e) {
         console.error("[Notifier] Docx generation failed:", e);
         return res.status(500).json({ success: false, error: "Failed to compile Word doc."});
    }

    // Determine target platform
    if (os.platform() !== 'win32') {
        console.log(`[Notifier] Not running on Windows. Simulating COM output bypassing Outlook interaction...`);
        // Fallback for my test Linux environment
        setTimeout(() => fs.unlinkSync(docxPath), 2000);
        return res.status(200).json({ success: true, message: "Simulated Email Save on Non-Windows Platform", docx: docxName });
    }

    // 1. Convert emails into semicolon-delimited blocks
    const toField = (recipients?.to || []).join(';');
    const ccField = (recipients?.cc || []).join(';');

    // 2. Escape HTML string accurately for Powershell literal casting
    const safeHtml = htmlBody.replace(/"/g, '""').replace(/\n/g, "");

    const psScript = `
$ErrorActionPreference = "Stop"
try {
    $Outlook = New-Object -ComObject Outlook.Application
    $Mail = $Outlook.CreateItem(0)
    $Mail.To = "${toField}"
    $Mail.CC = "${ccField}"
    $Mail.Subject = "${subject}"
    $Mail.HTMLBody = @"
${safeHtml}
"@
    $Mail.Attachments.Add('${docxPath}')
    $Mail.Save()  # Writes to drafts without blocking or requiring interaction
    Write-Output "SUCCESS"
} catch {
    Write-Error $_.Exception.Message
    exit 1
}
`;

    // 3. Write temp ps1
    const scriptPath = path.join(tempDir, `trigger-${Date.now()}.ps1`);
    fs.writeFileSync(scriptPath, psScript);

    // 4. Exec PowerShell silently bypassing policy
    const spawnString = `powershell.exe -ExecutionPolicy Bypass -File "${scriptPath}"`;
    
    console.log(`[Notifier] Interfacing with Outlook COM Service...`);
    exec(spawnString, (error, stdout, stderr) => {
        // Cleanup local temp resources regardless of mail output
        try { fs.unlinkSync(scriptPath); } catch(_) {}
        try { fs.unlinkSync(docxPath); } catch(_) {}

        if (error) {
            console.error(`[Notifier] Outlook Pipeline Failed: ${stderr || error.message}`);
            return res.status(500).json({ success: false, error: "PowerShell execution failed", stderr: stderr });
        }
        
        console.log(`[Notifier] Mail payload successfully dispatched to Outlook Drafts.`);
        res.status(200).json({ 
            success: true, 
            message: "Draft saved in Outlook gracefully.",
            stdout: stdout.trim() 
        });
    });
});

// For backward compatibility ping tests
app.post('/api/notify', (req, res) => {
    console.log("Received legacy webhook ping.");
    res.json({ success: true });
});

const PORT = 8081;
app.listen(PORT, '0.0.0.0', () => {
    console.log(`[Notifier Service] Running globally bounded on port ${PORT}...`);
    console.log(`[Notifier Service] Waiting for raw markdown payloads via POST /api/notify/email`);
});
