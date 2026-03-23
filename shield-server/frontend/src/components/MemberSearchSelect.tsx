import React, { useState, useEffect, useRef } from 'react';

interface Member {
  id: string;
  name: string;
  department?: string;
}

interface MemberSearchSelectProps {
  value: string;
  onChange: (memberId: string) => void;
  style?: React.CSSProperties;
}

/**
 * Searchable member selector: type to search, click to select.
 * Uses /api/members?search=xxx for server-side filtering.
 */
function MemberSearchSelect({ value, onChange, style }: MemberSearchSelectProps) {
  const [query, setQuery] = useState('');
  const [displayText, setDisplayText] = useState('');
  const [results, setResults] = useState<Member[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [loading, setLoading] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<number | null>(null);

  // When value changes externally, resolve the display name
  useEffect(() => {
    if (value && !displayText) {
      fetch(`/api/members?search=${encodeURIComponent(value)}&pageSize=5`)
        .then(res => res.json())
        .then(data => {
          const list: Member[] = data.items || [];
          const match = list.find((m: Member) => m.id === value);
          if (match) setDisplayText(`${match.name} (${match.id})`);
          else setDisplayText(value);
        })
        .catch(() => setDisplayText(value));
    }
  }, [value]);

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setShowDropdown(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const doSearch = (q: string) => {
    if (!q.trim()) {
      // Show recent/default list
      setLoading(true);
      fetch('/api/members?pageSize=20')
        .then(res => res.json())
        .then(data => setResults(data.items || []))
        .catch(() => setResults([]))
        .finally(() => setLoading(false));
      return;
    }
    setLoading(true);
    fetch(`/api/members?search=${encodeURIComponent(q)}&pageSize=20`)
      .then(res => res.json())
      .then(data => setResults(data.items || []))
      .catch(() => setResults([]))
      .finally(() => setLoading(false));
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setQuery(val);
    setShowDropdown(true);

    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => doSearch(val), 250);
  };

  const handleFocus = () => {
    setShowDropdown(true);
    doSearch(query);
  };

  const handleSelect = (member: Member) => {
    onChange(member.id);
    setDisplayText(`${member.name} (${member.id})`);
    setQuery('');
    setShowDropdown(false);
  };

  const handleClear = () => {
    onChange('');
    setDisplayText('');
    setQuery('');
    setResults([]);
  };

  return (
    <div ref={containerRef} style={{ position: 'relative', ...style }}>
      <div style={{ position: 'relative' }}>
        <input
          type="text"
          value={showDropdown ? query : (displayText || query)}
          onChange={handleInputChange}
          onFocus={handleFocus}
          placeholder={displayText || '输入姓名或工号搜索...'}
          style={{
            width: '100%', padding: '0.625rem 2rem 0.625rem 0.75rem', borderRadius: '6px',
            border: '1px solid var(--border-color)', background: 'var(--bg-color)',
            color: 'var(--text-color)', boxSizing: 'border-box', fontSize: '0.875rem',
            transition: 'border-color 0.2s', outline: 'none'
          }}
          onKeyDown={e => {
            if (e.key === 'Escape') setShowDropdown(false);
          }}
        />
        {/* Search icon or clear button */}
        {displayText ? (
          <button
            type="button"
            onClick={handleClear}
            style={{
              position: 'absolute', right: '0.5rem', top: '50%', transform: 'translateY(-50%)',
              background: 'transparent', border: 'none', cursor: 'pointer', padding: '2px',
              color: '#94a3b8', display: 'flex', alignItems: 'center'
            }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        ) : (
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ position: 'absolute', right: '0.75rem', top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none' }}>
            <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
          </svg>
        )}
      </div>

      {showDropdown && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, right: 0, marginTop: '4px',
          background: 'var(--card-bg)', border: '1px solid var(--border-color)',
          borderRadius: '8px', boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
          maxHeight: '240px', overflowY: 'auto', zIndex: 100
        }}>
          {loading ? (
            <div style={{ padding: '1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.8rem' }}>搜索中...</div>
          ) : results.length === 0 ? (
            <div style={{ padding: '1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.8rem' }}>
              {query ? '未找到匹配的人员' : '输入关键字开始搜索'}
            </div>
          ) : (
            results.map(m => (
              <div
                key={m.id}
                onClick={() => handleSelect(m)}
                style={{
                  padding: '0.5rem 0.75rem', cursor: 'pointer', fontSize: '0.875rem',
                  display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                  transition: 'background 0.15s',
                  background: m.id === value ? 'rgba(37,99,235,0.08)' : 'transparent',
                  color: 'var(--text-color)'
                }}
                onMouseEnter={e => e.currentTarget.style.background = 'var(--bg-color)'}
                onMouseLeave={e => e.currentTarget.style.background = m.id === value ? 'rgba(37,99,235,0.08)' : 'transparent'}
              >
                <span>
                  <span style={{ fontWeight: 500 }}>{m.name}</span>
                  <span style={{ color: '#94a3b8', marginLeft: '0.4rem', fontSize: '0.8rem' }}>({m.id})</span>
                </span>
                {m.department && (
                  <span style={{ color: '#94a3b8', fontSize: '0.75rem' }}>{m.department}</span>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export default MemberSearchSelect;
