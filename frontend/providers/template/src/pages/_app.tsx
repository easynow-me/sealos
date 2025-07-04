import Layout from '@/components/layout';
import { theme } from '@/constants/theme';
import { useGlobalStore } from '@/store/global';
import { getLangStore, setLangStore } from '@/utils/cookieUtils';
import { ChakraProvider } from '@chakra-ui/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import throttle from 'lodash/throttle';
import { appWithTranslation, useTranslation } from 'next-i18next';
import type { AppProps } from 'next/app';
import Head from 'next/head';
import Router, { useRouter } from 'next/router';
import NProgress from 'nprogress'; //nprogress module
import { useEffect, useState } from 'react';
import { EVENT_NAME } from 'sealos-desktop-sdk';
import { createSealosApp, sealosApp } from 'sealos-desktop-sdk/app';
import { useSystemConfigStore } from '@/store/config';
import useSessionStore from '@/store/session';
import { useUserStore } from '@/store/user';

import '@sealos/driver/src/driver.css';
import '@/styles/reset.scss';
import 'nprogress/nprogress.css';
import { useGuideStore } from '@/store/guide';

//Binding events.
Router.events.on('routeChangeStart', () => NProgress.start());
Router.events.on('routeChangeComplete', () => NProgress.done());
Router.events.on('routeChangeError', () => NProgress.done());

// Create a client
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: false,
      cacheTime: 0
    }
  }
});

const App = ({ Component, pageProps }: AppProps) => {
  const router = useRouter();
  const { setSession } = useSessionStore();
  const { i18n } = useTranslation();
  const { setScreenWidth, setLastRoute } = useGlobalStore();
  const { initSystemConfig, initSystemEnvs } = useSystemConfigStore();
  const [refresh, setRefresh] = useState(false);
  const { loadUserSourcePrice } = useUserStore();
  const brandName = process.env.NEXT_PUBLIC_BRAND_NAME || 'Sealos';

  useEffect(() => {
    initSystemConfig(i18n.language);
    loadUserSourcePrice();
  }, []);

  useEffect(() => {
    NProgress.start();
    const response = createSealosApp();
    (async () => {
      try {
        const res = await sealosApp.getSession();
        setSession(res);
      } catch (err) {
        console.log('App is not running in desktop');
      }
    })();
    localStorage.removeItem('session');
    NProgress.done();
    return response;
  }, []);

  // add resize event
  useEffect(() => {
    const resize = throttle((e: Event) => {
      const documentWidth = document.documentElement.clientWidth || document.body.clientWidth;
      setScreenWidth(documentWidth);
    }, 200);
    window.addEventListener('resize', resize);
    const documentWidth = document.documentElement.clientWidth || document.body.clientWidth;
    setScreenWidth(documentWidth);

    return () => {
      window.removeEventListener('resize', resize);
    };
  }, [setScreenWidth]);

  useEffect(() => {
    const changeI18n = async (data: any) => {
      const lastLang = getLangStore();
      const newLang = data.currentLanguage;
      if (lastLang !== newLang && i18n?.changeLanguage) {
        i18n.changeLanguage(newLang);
        setLangStore(newLang);
        setRefresh((state) => !state);
      }
    };
    (async () => {
      try {
        const lang = await sealosApp.getLanguage();

        changeI18n({
          currentLanguage: lang.lng
        });
      } catch (error) {
        console.warn('get desktop getLanguage error');
      }
    })();
    return sealosApp?.addAppEventListen(EVENT_NAME.CHANGE_I18N, changeI18n);
  }, []);

  // record route
  useEffect(() => {
    return () => {
      setLastRoute(router.asPath);
    };
  }, [router.pathname]);

  useEffect(() => {
    const lang = getLangStore();
    if (lang) {
      i18n?.changeLanguage?.(lang);
    }
  }, [i18n, refresh, router.pathname]);

  useEffect(() => {
    const setupInternalAppCallListener = async () => {
      try {
        const envs = await initSystemEnvs();
        const event = async (
          e: MessageEvent<{
            name: string;
            type: string;
            action?: string;
          }>
        ) => {
          const whitelist = [`https://${envs.DESKTOP_DOMAIN}`];
          if (!whitelist.includes(e.origin)) {
            return;
          }
          try {
            if (e.data.type === 'InternalAppCall' && e.data?.name) {
              router.push({
                pathname: '/instance',
                query: {
                  instanceName: e.data.name
                }
              });
            }
            if (e.data?.action === 'guide') {
              router.push({
                pathname: '/'
              });
              useGuideStore.getState().resetGuideState(false);
            }
          } catch (error) {
            console.log(error, 'error');
          }
        };
        window.addEventListener('message', event);
        return () => window.removeEventListener('message', event);
      } catch (error) {}
    };
    setupInternalAppCallListener();
  }, []);

  return (
    <>
      <Head>
        <title>{brandName} Templates</title>
        <meta name="description" content={`Generated by ${brandName} Team`} />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <link rel="icon" href="/favicon.ico" />
      </Head>
      <QueryClientProvider client={queryClient}>
        <ChakraProvider theme={theme}>
          <Layout>
            <Component {...pageProps} />
          </Layout>
        </ChakraProvider>
      </QueryClientProvider>
    </>
  );
};

export default appWithTranslation(App);
