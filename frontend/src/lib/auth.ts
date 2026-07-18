import { useState, useEffect } from 'react';

const TOKEN_KEY = 'kiwi_token';
const ORG_ID_KEY = 'kiwi_org_id';
const ORG_NAME_KEY = 'kiwi_org_name';

export const auth = {
  getToken: () => {
    if (typeof window !== 'undefined') {
      return localStorage.getItem(TOKEN_KEY);
    }
    return null;
  },
  setSession: (token: string, orgId: string, orgName: string) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem(TOKEN_KEY, token);
      localStorage.setItem(ORG_ID_KEY, orgId);
      localStorage.setItem(ORG_NAME_KEY, orgName);
    }
  },
  clearSession: () => {
    if (typeof window !== 'undefined') {
      localStorage.removeItem(TOKEN_KEY);
      localStorage.removeItem(ORG_ID_KEY);
      localStorage.removeItem(ORG_NAME_KEY);
    }
  },
  getOrgName: () => {
    if (typeof window !== 'undefined') {
      return localStorage.getItem(ORG_NAME_KEY);
    }
    return null;
  }
};

export function useAuth() {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean | null>(null);

  useEffect(() => {
    const token = auth.getToken();
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setIsAuthenticated(!!token);
  }, []);

  return { 
    isAuthenticated, 
    logout: () => {
      auth.clearSession();
      window.location.href = '/login';
    } 
  };
}
