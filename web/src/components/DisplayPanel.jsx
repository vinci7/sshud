import { useState } from 'react';
import { HeaderGlobalAction, HeaderPanel, RadioButton, RadioButtonGroup, Stack } from '@carbon/react';
import { ColorPalette } from '@carbon/icons-react';
import { t } from '../lib/i18n.js';
import { langChoice } from '../lib/prefs.js';

// DisplayPanel is the header panel for theme and language.
export default function DisplayPanel({ theme, setTheme }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <HeaderGlobalAction aria-label={t('display.title')} tooltipAlignment="end" isActive={open} onClick={() => setOpen(!open)}>
        <ColorPalette size={20} />
      </HeaderGlobalAction>
      <HeaderPanel expanded={open} aria-label={t('display.title')}>
        <Stack gap={7} className="sb-display__body">
          <RadioButtonGroup
            legendText={t('display.theme')}
            name="theme"
            orientation="vertical"
            valueSelected={theme}
            onChange={(v) => {
              localStorage.setItem('theme', v);
              setTheme(v);
            }}
          >
            <RadioButton id="theme-system" value="system" labelText={t('display.system')} />
            <RadioButton id="theme-dark" value="dark" labelText={t('display.dark')} />
            <RadioButton id="theme-light" value="light" labelText={t('display.light')} />
          </RadioButtonGroup>
          <RadioButtonGroup
            legendText={t('display.language')}
            name="lang"
            orientation="vertical"
            valueSelected={langChoice()}
            onChange={(v) => {
              localStorage.setItem('lang', v);
              window.location.reload(); // strings are resolved once at load
            }}
          >
            <RadioButton id="lang-auto" value="auto" labelText={t('display.browser')} />
            <RadioButton id="lang-en" value="en" labelText="English" />
            <RadioButton id="lang-zh" value="zh" labelText="简体中文" />
          </RadioButtonGroup>
        </Stack>
      </HeaderPanel>
    </>
  );
}
