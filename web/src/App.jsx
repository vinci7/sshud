import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { Button, Content, Header, HeaderGlobalAction, HeaderGlobalBar, HeaderName, InlineNotification, Tag, Theme, Tile } from '@carbon/react';
import { Add, Logout, Settings } from '@carbon/icons-react';
import DisplayPanel from './components/DisplayPanel.jsx';
import Login from './pages/Login.jsx';
import Overview from './pages/Overview.jsx';
// the detail page carries Carbon Charts and the management page is rarely
// opened, so neither is part of the first load
const Detail = lazy(() => import('./pages/Detail.jsx'));
const Machines = lazy(() => import('./pages/Machines.jsx'));
import { onStreamClosed, useJSON } from './lib/api.js';
import { t } from './lib/i18n.js';
import { matchMachine, navigate, onLinkClick, usePath, useTitle } from './lib/router.js';
import { CarbonTheme, themeChoice, useCarbonTheme } from './lib/prefs.js';

function Shell({ carbon, actions, demo, children }) {
  return (
    <Theme theme={carbon} className="sb-app">
      <Header aria-label={t('app.name')}>
        <HeaderName href="/" prefix="" onClick={onLinkClick}>
          {t('app.name')}
        </HeaderName>
        {demo && (
          <Tag type="blue" size="sm" className="sb-header-tag">
            {t('demo')}
          </Tag>
        )}
        <HeaderGlobalBar>{actions}</HeaderGlobalBar>
      </Header>
      <Content className="sb-content">{children}</Content>
    </Theme>
  );
}

// useMachines loads the machine list and can be told to reload after edits.
function useMachines() {
  const [state, setState] = useState({ data: null, error: null });
  const load = useCallback(() => {
    fetch('/api/machines')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.statusText))))
      .then((data) => setState({ data, error: null }))
      .catch((error) => setState((s) => ({ ...s, error })));
  }, []);
  useEffect(load, [load]);
  return { ...state, reload: load };
}

function Empty() {
  return (
    <Tile className="sb-empty">
      <h3>{t('empty.title')}</h3>
      <p className="sb-muted">{t('empty.body')}</p>
      <Button renderIcon={Add} onClick={() => navigate('/machines')}>
        {t('manage.add')}
      </Button>
    </Tile>
  );
}

export default function App() {
  const [metaKey, setMetaKey] = useState(0);
  const [theme, setTheme] = useState(themeChoice);
  const carbon = useCarbonTheme(theme);
  const meta = useJSON(`/api/meta?k=${metaKey}`);
  useEffect(() => onStreamClosed(() => setMetaKey((k) => k + 1)), []);
  const display = <DisplayPanel theme={theme} setTheme={setTheme} />;

  let body;
  if (!meta.data && !meta.error) body = <Theme theme={carbon} className="sb-app" />;
  else if (meta.data && !meta.data.loggedIn) {
    body = (
      <Shell carbon={carbon} actions={display}>
        <Login setup={meta.data.setup} onDone={() => setMetaKey((k) => k + 1)} />
      </Shell>
    );
  } else body = <Main meta={meta} carbon={carbon} display={display} onLogout={() => setMetaKey((k) => k + 1)} />;
  return <CarbonTheme.Provider value={carbon}>{body}</CarbonTheme.Provider>;
}

function Main({ meta, carbon, display, onLogout }) {
  const machines = useMachines();
  const path = usePath();
  const failed = meta.error || machines.error;
  const id = matchMachine(path);
  const machine = id && machines.data?.find((m) => m.id === id);
  const list = machines.data;
  useTitle(path === '/machines' ? t('manage.title') : machine?.name);

  let page = null;
  if (path === '/machines') page = <Machines meta={meta.data} onChanged={machines.reload} />;
  else if (id) {
    if (machine) page = <Detail key={machine.id} machine={machine} readOnly={meta.data?.readOnly} />;
    else if (list) page = <InlineNotification kind="warning" title={t('detail.notFound')} hideCloseButton />;
  } else if (list) page = list.length ? <Overview machines={list} /> : <Empty />;

  return (
    <Shell
      carbon={carbon}
      demo={meta.data?.demo}
      actions={
        <>
          {display}
          <HeaderGlobalAction
            aria-label={t('manage.title')}
            tooltipAlignment="end"
            isActive={path === '/machines'}
            onClick={() => navigate('/machines')}
          >
            <Settings size={20} />
          </HeaderGlobalAction>
          {meta.data?.auth === 'password' && (
            <HeaderGlobalAction
              aria-label={t('login.logout')}
              tooltipAlignment="end"
              onClick={() => fetch('/api/logout', { method: 'POST' }).then(onLogout)}
            >
              <Logout size={20} />
            </HeaderGlobalAction>
          )}
        </>
      }
    >
      {failed && <InlineNotification kind="error" title={t('app.unreachable')} hideCloseButton />}
      <Suspense fallback={null}>{page}</Suspense>
    </Shell>
  );
}
