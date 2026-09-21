import React, { useEffect, useMemo } from 'react';
import { ConfigProvider, theme as antTheme } from 'antd';
import { getThemePreset } from '@/components/Theme/themePresets';
import { applyThemeToDOM, useThemeStore } from '@/stores/useThemeStore';

interface ThemeProviderProps {
  children: React.ReactNode;
}

const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const themeId = useThemeStore((s) => s.theme);

  useEffect(() => {
    applyThemeToDOM(themeId);
  }, [themeId]);

  const antdThemeConfig = useMemo(() => {
    const preset = getThemePreset(themeId);
    const { colors } = preset;
    const isDark = preset.mode === 'dark';
    const isFeishuStyle = preset.id === 'feishu';
    return {
      algorithm: isDark ? antTheme.darkAlgorithm : antTheme.defaultAlgorithm,
      token: {
        colorPrimary: colors.primary,
        colorPrimaryHover: colors.primaryHover,
        colorTextLightSolid: colors.onPrimary,
        colorBgContainer: colors.bgCard,
        colorBgElevated: colors.bgElevated,
        colorBgLayout: colors.bgVoid,
        colorBorder: colors.border,
        colorBorderSecondary: colors.borderLit,
        colorText: colors.text,
        colorTextSecondary: colors.textSecondary,
        colorTextTertiary: colors.textDim,
        colorTextQuaternary: colors.borderLit,
        colorFill: colors.border,
        colorFillSecondary: colors.bgElevated,
        colorFillTertiary: colors.bgCard,
        colorFillQuaternary: colors.bgSurface,
        controlOutline: `color-mix(in srgb, ${colors.primary} 55%, transparent)`,
        borderRadius: isFeishuStyle ? 8 : 10,
        borderRadiusSM: isFeishuStyle ? 6 : 8,
        borderRadiusLG: isFeishuStyle ? 10 : 14,
        controlHeight: 38,
        controlHeightSM: 32,
        controlHeightLG: 44,
        fontSize: 14,
        fontSizeSM: 12,
        fontFamily: isFeishuStyle
          ? "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
          : "'Noto Sans SC', 'Space Grotesk', sans-serif",
      },
      components: {
        Layout: {
          headerBg: colors.bgSurface,
          siderBg: colors.bgSurface,
          bodyBg: colors.bgVoid,
        },
        Menu: {
          darkItemBg: colors.bgSurface,
          darkSubMenuItemBg: colors.bgVoid,
          itemBg: colors.bgSurface,
        },
        Card: {
          colorBgContainer: colors.bgCard,
        },
        Modal: {
          contentBg: colors.bgCard,
          headerBg: colors.bgCard,
        },
        Input: {
          colorBgContainer: isDark ? colors.bgElevated : colors.bgCard,
        },
        Button: {
          colorPrimary: colors.primary,
          algorithm: true,
        },
      },
    };
  }, [themeId]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {children}
    </ConfigProvider>
  );
};

export default ThemeProvider;
