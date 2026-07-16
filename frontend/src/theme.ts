import type { ThemeConfig } from 'antd';

export const ACCENT = {
  mint: '#1f6f63',
  coral: '#b8543d',
  gold: '#d69a3a',
  violet: '#6b5bd2',
  ink: '#24383b',
};

export const THEME: ThemeConfig = {
  token: {
    colorPrimary: ACCENT.mint,
    colorInfo: ACCENT.mint,
    colorSuccess: ACCENT.mint,
    colorWarning: ACCENT.gold,
    colorError: ACCENT.coral,
    colorTextBase: '#18201f',
    colorBgContainer: 'rgba(251, 253, 247, 0.94)',
    colorBgElevated: 'rgba(251, 253, 247, 0.98)',
    borderRadius: 10,
    fontFamily: '"Aptos", "Segoe UI", sans-serif',
  },
  components: {
    Layout: { headerBg: 'transparent', bodyBg: 'transparent' },
    Card: { boxShadowTertiary: '0 18px 45px rgba(32, 42, 38, 0.12)' },
  },
};
