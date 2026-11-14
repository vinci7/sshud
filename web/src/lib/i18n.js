// UI strings live in src/locales/<lang>.json. English is the default;
// Chinese follows the browser language or the saved choice.
import en from '../locales/en.json';
import zh from '../locales/zh-CN.json';

const messages = { en, zh };

function pickLang() {
  const saved = localStorage.getItem('lang');
  if (messages[saved]) return saved; // "auto" or unset falls through
  return (navigator.language || 'en').toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

export const lang = pickLang();

// t looks up a key and fills {name} placeholders from vars.
export function t(key, vars) {
  let s = messages[lang][key] ?? en[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replace(`{${k}}`, v);
  return s;
}
