/**
 * Convert a SSH/SCP git URL to a browser-openable HTTPS URL.
 *
 * Handles:
 *   git@host:path/repo.git            →  https://host/path/repo
 *   git@host:PORT/path/repo.git       →  https://host/path/repo  (port dropped)
 *   ssh://git@host/path.git           →  https://host/path
 *   ssh://git@host:PORT/path.git      →  https://host/path       (port dropped)
 *   ssh:git@host:PORT/path.git        →  https://host/path       (non-standard prefix)
 *   https://...                       →  unchanged
 *
 * Post-processing on the hostname:
 *   -git-  →  -   (e.g. my-git-server.example.com → my-server.example.com)
 */
export function sshToHttps(url: string): string {
  if (!url) return '';
  if (url.startsWith('http://') || url.startsWith('https://')) return url;

  let host = '';
  let repoPath = '';

  // Match ssh:// or ssh: (with optional //) prefix, then optional user@, host, optional :port, /path
  const protoMatch = url.match(/^ssh:\/{0,2}(?:[^@]+@)?([^/:]+)(?::\d+)?\/(.+?)(?:\.git)?$/);
  if (protoMatch) {
    host = protoMatch[1];
    repoPath = protoMatch[2];
  } else {
    // SCP format: [user@]host:path  or  [user@]host:PORT/path (port only when followed by /)
    const scpMatch = url.match(/^(?:[^@]+@)?([^:]+):(?:\d+\/)?(.+?)(?:\.git)?$/);
    if (scpMatch) {
      host = scpMatch[1];
      repoPath = scpMatch[2];
    } else {
      return url; // Unknown format, return as-is
    }
  }

  // Strip -git- from hostname → -
  host = host.replace(/-git-/g, '-');

  return `https://${host}/${repoPath}`;
}
