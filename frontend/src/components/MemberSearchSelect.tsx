import React, { useCallback } from 'react';
import { MemberSearchSelect as CommonMemberSearchSelect, type User, AUTH_TOKEN_KEY } from '@code/common';
import { apiUrl } from '../config';

export interface MemberSearchSelectProps {
  value: number | string | '';
  onChange: (userId: number | '', selectedUser?: User) => void;
  style?: React.CSSProperties;
}

export default function MemberSearchSelect({ value, onChange, style }: MemberSearchSelectProps) {
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
    <CommonMemberSearchSelect
      value={value}
      onChange={onChange}
      style={style}
      fetchFn={authFetch}
      searchEndpoint="/api/users"
      meEndpoint="/api/me"
    />
  );
}

