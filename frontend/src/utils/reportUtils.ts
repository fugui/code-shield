import { CanonicalSeverity, SeverityMeta } from '../types/report';
import { sshToHttps } from './urlUtils';

export const SEVERITY_CONFIG: Record<CanonicalSeverity, SeverityMeta> = {
  fatal: {
    key: 'fatal',
    label: '致命',
    color: '#ef4444',
    bg: 'rgba(239, 68, 68, 0.1)',
    weight: 100,
  },
  critical: {
    key: 'critical',
    label: '严重',
    color: '#f97316',
    bg: 'rgba(249, 115, 22, 0.1)',
    weight: 80,
  },
  major: {
    key: 'major',
    label: '一般',
    color: '#eab308',
    bg: 'rgba(234, 179, 8, 0.1)',
    weight: 60,
  },
  minor: {
    key: 'minor',
    label: '提示',
    color: '#3b82f6',
    bg: 'rgba(59, 130, 246, 0.1)',
    weight: 40,
  },
  suggestion: {
    key: 'suggestion',
    label: '建议',
    color: '#6b7280',
    bg: 'rgba(107, 114, 128, 0.1)',
    weight: 20,
  },
  pass: {
    key: 'pass',
    label: '合格',
    color: '#10b981',
    bg: 'rgba(16, 185, 129, 0.1)',
    weight: 0,
  },
};

export const normalizeSeverity = (raw?: string): CanonicalSeverity => {
  if (!raw) return 'minor';
  const s = raw.trim().toLowerCase();
  switch (s) {
    case 'fatal':
    case '致命':
    case '阻塞':
    case 'blocking':
    case 'p0':
      return 'fatal';
    case 'critical':
    case '严重':
    case '高风险':
    case 'high':
    case 'high_risk':
    case 'p1':
      return 'critical';
    case 'major':
    case '一般':
    case '中风险':
    case 'medium':
    case '主要':
    case 'p2':
      return 'major';
    case 'minor':
    case '提示':
    case '低风险':
    case 'low':
    case '次要':
    case 'p3':
    case 'info':
      return 'minor';
    case 'suggestion':
    case '建议':
      return 'suggestion';
    case 'pass':
    case '合格':
    case '通过':
      return 'pass';
    default:
      if (s.includes('致命') || s.includes('阻塞')) return 'fatal';
      if (s.includes('严重') || s.includes('高')) return 'critical';
      if (s.includes('一般') || s.includes('中')) return 'major';
      if (s.includes('合格') || s.includes('通过')) return 'pass';
      return 'minor';
  }
};

export const getSeverityMeta = (raw?: string): SeverityMeta => {
  const key = normalizeSeverity(raw);
  return SEVERITY_CONFIG[key] || SEVERITY_CONFIG.minor;
};

export const formatDuration = (seconds?: number): string => {
  if (seconds == null || isNaN(seconds) || seconds <= 0) return '-';
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`;
  const s = Math.round(seconds);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
};

export const getLocationText = (filePath: string, lineNumber?: string | number): string => {
  if (!filePath) return '';
  const parts = filePath.split(/[/\\]/);
  const fileName = parts[parts.length - 1] || filePath;
  return lineNumber ? `${fileName}:${lineNumber}` : fileName;
};

export const getRepoSourceUrl = (
  repoUrl?: string,
  branch?: string,
  filePath?: string,
  lineNumber?: string | number
): string => {
  if (!repoUrl || !filePath) return '';
  const webUrl = sshToHttps(repoUrl);
  const targetBranch = branch ? branch.trim() : 'master';
  const encodedFilePath = encodeURIComponent(filePath);
  const encodedBranch = encodeURIComponent(targetBranch);

  let fileUrl = `${webUrl}/files?ref=${encodedBranch}&filePath=${encodedFilePath}&isFile=true`;
  if (lineNumber) {
    const cleanLine = lineNumber.toString().replace(/\s+/g, '');
    const firstLineMatch = cleanLine.match(/^([0-9]+)/);
    if (firstLineMatch) {
      fileUrl += `#L${firstLineMatch[1]}`;
    }
  }
  return fileUrl;
};

export const copyToClipboardWithFallback = async (text: string): Promise<boolean> => {
  if (!text) return false;
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fallback
    }
  }

  // 兜底方案
  try {
    const textArea = document.createElement('textarea');
    textArea.value = text;
    textArea.style.position = 'fixed';
    textArea.style.left = '-999999px';
    textArea.style.top = '-999999px';
    document.body.appendChild(textArea);
    textArea.focus();
    textArea.select();
    const successful = document.execCommand('copy');
    document.body.removeChild(textArea);
    return successful;
  } catch {
    return false;
  }
};
