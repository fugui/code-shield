import React, { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter';
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { Check, Copy } from 'lucide-react';

// 常用语法高亮支持
import go from 'react-syntax-highlighter/dist/esm/languages/prism/go';
import python from 'react-syntax-highlighter/dist/esm/languages/prism/python';
import javascript from 'react-syntax-highlighter/dist/esm/languages/prism/javascript';
import typescript from 'react-syntax-highlighter/dist/esm/languages/prism/typescript';
import java from 'react-syntax-highlighter/dist/esm/languages/prism/java';
import c from 'react-syntax-highlighter/dist/esm/languages/prism/c';
import cpp from 'react-syntax-highlighter/dist/esm/languages/prism/cpp';
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json';
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql';
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash';
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml';
import rust from 'react-syntax-highlighter/dist/esm/languages/prism/rust';

SyntaxHighlighter.registerLanguage('go', go);
SyntaxHighlighter.registerLanguage('python', python);
SyntaxHighlighter.registerLanguage('javascript', javascript);
SyntaxHighlighter.registerLanguage('typescript', typescript);
SyntaxHighlighter.registerLanguage('java', java);
SyntaxHighlighter.registerLanguage('c', c);
SyntaxHighlighter.registerLanguage('cpp', cpp);
SyntaxHighlighter.registerLanguage('json', json);
SyntaxHighlighter.registerLanguage('sql', sql);
SyntaxHighlighter.registerLanguage('bash', bash);
SyntaxHighlighter.registerLanguage('yaml', yaml);
SyntaxHighlighter.registerLanguage('rust', rust);

const SUPPORTED_LANGUAGES = new Set([
  'c',
  'cpp',
  'go',
  'python',
  'javascript',
  'typescript',
  'java',
  'json',
  'sql',
  'bash',
  'yaml',
  'rust',
]);

const LANGUAGE_ALIASES: Record<string, string> = {
  'c++': 'cpp',
  'cxx': 'cpp',
  'cc': 'cpp',
  'h': 'cpp',
  'hpp': 'cpp',
  'py': 'python',
  'js': 'javascript',
  'ts': 'typescript',
  'sh': 'bash',
  'shell': 'bash',
  'zsh': 'bash',
  'yml': 'yaml',
  'rs': 'rust',
  'golang': 'go',
};

interface CodeBlockProps {
  language?: string;
  code: string;
}

function CodeBlock({ language = '', code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    if (!code) return;
    navigator.clipboard
      .writeText(code)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      })
      .catch(() => {});
  };

  const rawLang = language.trim().toLowerCase();
  const normalizedLang = LANGUAGE_ALIASES[rawLang] || rawLang;
  const isSupported = SUPPORTED_LANGUAGES.has(normalizedLang);
  const displayLang = (rawLang || 'CODE').toUpperCase();

  return (
    <div className="code-block-container">
      <div className="code-block-header">
        <span className="code-block-lang-badge">{displayLang}</span>
        <button
          type="button"
          className="code-block-copy-btn"
          onClick={handleCopy}
          title="复制代码片段"
        >
          {copied ? (
            <>
              <Check size={13} style={{ color: '#10b981' }} />
              <span style={{ color: '#10b981' }}>已复制</span>
            </>
          ) : (
            <>
              <Copy size={13} />
              <span>复制</span>
            </>
          )}
        </button>
      </div>
      <div className="code-block-body">
        {isSupported ? (
          <SyntaxHighlighter
            style={vscDarkPlus}
            language={normalizedLang}
            PreTag="div"
            customStyle={{
              margin: 0,
              padding: '0.85rem 1.1rem',
              fontSize: '0.83rem',
              lineHeight: '1.6',
              fontFamily: '"JetBrains Mono", Consolas, "Fira Code", monospace',
              background: '#0f172a',
              borderRadius: '0 0 8px 8px',
            }}
          >
            {code}
          </SyntaxHighlighter>
        ) : (
          <pre
            style={{
              margin: 0,
              padding: '0.85rem 1.1rem',
              fontSize: '0.83rem',
              lineHeight: '1.6',
              fontFamily: '"JetBrains Mono", Consolas, "Fira Code", monospace',
              background: '#0f172a',
              color: '#e2e8f0',
              overflowX: 'auto',
              borderRadius: '0 0 8px 8px',
            }}
          >
            <code>{code}</code>
          </pre>
        )}
      </div>
    </div>
  );
}

/**
 * 预处理建议文本：规范化反引号代码块换行，确保标准 Markdown 渲染引擎能够正确识别行内与块级代码
 */
function preprocessMarkdown(text: string): string {
  if (!text) return '';
  // 保证非换行字符后的三反引号前有换行
  let formatted = text.replace(/([^\n])\s*```/g, '$1\n\n```');
  // 保证闭合的三反引号前有换行
  formatted = formatted.replace(/([^\n])```/g, '$1\n```');
  return formatted;
}

interface SuggestionMarkdownProps {
  content: string;
  className?: string;
}

export default function SuggestionMarkdown({ content, className = '' }: SuggestionMarkdownProps) {
  if (!content) return null;

  const processed = preprocessMarkdown(content);

  return (
    <div className={`suggestion-content ${className}`.trim()}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          pre({ children }) {
            return <div className="suggestion-code-container">{children}</div>;
          },
          code({ className: codeClassName, children, ...props }) {
            const rawText = String(children);
            const isMultiline = rawText.includes('\n');
            const match = /language-([^\s]+)/.exec(codeClassName || '');
            const isBlock = Boolean(match || isMultiline);

            if (isBlock) {
              const codeString = rawText.replace(/\n$/, '');
              return <CodeBlock language={match ? match[1] : ''} code={codeString} />;
            }

            return (
              <code className="suggestion-inline-code" {...props}>
                {children}
              </code>
            );
          },
        }}
      >
        {processed}
      </ReactMarkdown>
    </div>
  );
}
