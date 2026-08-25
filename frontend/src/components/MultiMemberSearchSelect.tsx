import React, { useCallback } from 'react';
import { MultiMemberSearchSelect as CommonMultiMemberSearchSelect, MultiMemberSearchSelectProps as CommonProps, AUTH_TOKEN_KEY } from '@code/common';
import { apiUrl } from '../config';

export interface MultiMemberSearchSelectProps {
  value: string[];
  onChange: (memberIds: string[]) => void;
  style?: React.CSSProperties;
  maxSelections?: number;
}

export default function MultiMemberSearchSelect({ value, onChange, style, maxSelections }: MultiMemberSearchSelectProps) {
  const authFetch = useCallback((url: string, options: RequestInit = {}) => {
    const token = typeof window !== 'undefined' ? localStorage.getItem(AUTH_TOKEN_KEY) : null;
    const headers: Record<string, string> = {
      ...(options.headers as Record<string, string> || {})
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
    const finalUrl = url.startsWith('/api') ? apiUrl(url) : url;
    return fetch(finalUrl, {
      ...options,
      headers
    });
  }, []);

  return (
    <CommonMultiMemberSearchSelect
      value={value}
      onChange={onChange}
      style={style}
      maxSelections={maxSelections}
      fetchFn={authFetch}
      searchEndpoint="/api/users"
    />
  );
}

