import React, { useState, useEffect, useRef } from 'react';

interface Member {
  id: string;
  name: string;
  department?: string;
}

interface MultiMemberSearchSelectProps {
  value: string[]; // array of member IDs
  onChange: (memberIds: string[]) => void;
  style?: React.CSSProperties;
  maxSelections?: number;
}

export default function MultiMemberSearchSelect({ value = [], onChange, style, maxSelections = 20 }: MultiMemberSearchSelectProps) {
  const [query, setQuery] = useState('');
  const [selectedMembers, setSelectedMembers] = useState<Member[]>([]);
  const [results, setResults] = useState<Member[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [loading, setLoading] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<number | null>(null);

  // Initialize selected members from value array
  useEffect(() => {
    if (value && value.length > 0) {
      // Find members missing in our current state to avoid excessive fetching
      const missingIds = value.filter(id => !selectedMembers.some(m => m.id === id));
      if (missingIds.length > 0) {
        // Fetch missing details
        // Note: For a robust system we'd support fetching multiple IDs, but here we can just fallback to the ID itself
        // or trigger independent fetches if we really want names. For simplicity we'll just populate placeholder names.
        const newMembers = missingIds.map(id => ({ id, name: `User (${id})` }));
        // Ideally, we'd fetch /api/members?search=... for each if needed, but assuming they are loaded in parents.
        // Let's do a best-effort simple map for now unless results have them.
        setSelectedMembers(prev => {
          const combined = [...prev];
          missingIds.forEach(id => {
            const foundInResults = results.find(r => r.id === id);
            if (foundInResults && !combined.some(c => c.id === id)) {
              combined.push(foundInResults);
            } else if (!combined.some(c => c.id === id)) {
              // Temporary placeholder
              combined.push({ id, name: `User (${id})` });
            }
          });
          return combined;
        });
      }
    } else {
        setSelectedMembers([]);
    }
  }, [value, results]);

  // Optionally fetch fully resolved members on mount if value is not empty (async cleanup)
  useEffect(() => {
    if (value && value.length > 0) {
      // Just a background refresh of the first 20 members which usually covers the top ones mapped
      fetch('/api/members?pageSize=1000')
        .then(res => res.json())
        .then(data => {
            const list: Member[] = data.items || [];
            setSelectedMembers(prev => {
                return value.map(id => {
                    const match = list.find(m => m.id === id);
                    return match || prev.find(p => p.id === id) || { id, name: id };
                });
            });
        }).catch(() => {});
    }
  }, []);

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
    setLoading(true);
    const url = q.trim() ? `/api/members?search=${encodeURIComponent(q)}&pageSize=20` : '/api/members?pageSize=20';
    fetch(url)
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
    if (value.includes(member.id)) return; // Already selected
    if (value.length >= maxSelections) {
      alert(`最多只能选择 ${maxSelections} 名相关人员`);
      return;
    }
    const newValues = [...value, member.id];
    onChange(newValues);
    setQuery('');
    // Optionally close or keep open? Keeping open is better for multi-select.
  };

  const handleRemove = (idToRemove: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const newValues = value.filter(id => id !== idToRemove);
    onChange(newValues);
  };

  return (
    <div ref={containerRef} style={{ position: 'relative', ...style }}>
      <div 
        style={{ 
          display: 'flex', flexWrap: 'wrap', gap: '0.25rem', alignItems: 'center',
          minHeight: '40px', padding: '0.3rem 0.5rem', borderRadius: '6px',
          border: '1px solid var(--border-color)', background: 'var(--bg-color)',
          transition: 'border-color 0.2s', boxSizing: 'border-box'
        }}
        onClick={() => setShowDropdown(true)}
      >
        {selectedMembers.filter(m => value.includes(m.id)).map(m => (
          <span 
            key={m.id} 
            style={{ 
              display: 'inline-flex', alignItems: 'center', background: 'var(--primary-color)', 
              color: 'white', borderRadius: '4px', padding: '2px 6px', fontSize: '0.75rem' 
            }}
          >
            {m.name}
            <span 
              onClick={(e) => handleRemove(m.id, e)}
              style={{ marginLeft: '4px', cursor: 'pointer', opacity: 0.8 }}
            >
              ×
            </span>
          </span>
        ))}
        <input
          type="text"
          value={query}
          onChange={handleInputChange}
          onFocus={handleFocus}
          placeholder={value.length === 0 ? "输入姓名或工号搜索相关人员..." : ""}
          style={{
            flex: 1, minWidth: '120px', border: 'none', background: 'transparent',
            outline: 'none', color: 'var(--text-color)', fontSize: '0.875rem',
            padding: '2px'
          }}
          onKeyDown={e => {
            if (e.key === 'Escape') setShowDropdown(false);
          }}
        />
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
            results.map(m => {
              const isSelected = value.includes(m.id);
              return (
                <div
                  key={m.id}
                  onClick={() => handleSelect(m)}
                  style={{
                    padding: '0.5rem 0.75rem', cursor: isSelected ? 'default' : 'pointer', fontSize: '0.875rem',
                    display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                    transition: 'background 0.15s',
                    background: isSelected ? 'rgba(37,99,235,0.08)' : 'transparent',
                    color: isSelected ? '#94a3b8' : 'var(--text-color)',
                    opacity: isSelected ? 0.6 : 1
                  }}
                  onMouseEnter={e => { if (!isSelected) e.currentTarget.style.background = 'var(--bg-color)' }}
                  onMouseLeave={e => { if (!isSelected) e.currentTarget.style.background = 'transparent' }}
                >
                  <span>
                    <span style={{ fontWeight: 500 }}>{m.name}</span>
                    <span style={{ marginLeft: '0.4rem', fontSize: '0.8rem' }}>({m.id})</span>
                  </span>
                  {m.department && (
                    <span style={{ fontSize: '0.75rem' }}>{m.department}</span>
                  )}
                </div>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
