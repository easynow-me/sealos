import { getInitData } from '@/api/platform';
import { Coin } from '@/constants/app';

export let SEALOS_DOMAIN = 'cloud.sealos.io';
export let SEALOS_USER_DOMAINS = [{ name: 'cloud.sealos.io', secretName: 'wildcard-cert' }];
export let DESKTOP_DOMAIN = 'cloud.sealos.io';
export let DOMAIN_PORT = '';
export let SHOW_EVENT_ANALYZE = false;
export let CURRENCY = Coin.shellCoin;
export let UPLOAD_LIMIT = 50;
export let DOWNLOAD_LIMIT = 100;
export let PVC_STORAGE_MAX = 20;
export let GPU_ENABLED = false;
export let ISTIO_ENABLED = false;
export let ISTIO_PUBLIC_DOMAINS: string[] = [];
export let ISTIO_SHARED_GATEWAY = 'sealos-gateway';
export let ISTIO_ENABLE_TRACING = false;

export const loadInitData = async () => {
  try {
    const res = await getInitData();

    SEALOS_DOMAIN = res.SEALOS_DOMAIN;
    SEALOS_USER_DOMAINS = res.SEALOS_USER_DOMAINS;
    DOMAIN_PORT = res.DOMAIN_PORT;
    SHOW_EVENT_ANALYZE = res.SHOW_EVENT_ANALYZE;
    CURRENCY = res.CURRENCY;
    UPLOAD_LIMIT = res.fileMangerConfig.uploadLimit;
    DOWNLOAD_LIMIT = res.fileMangerConfig.downloadLimit;
    DESKTOP_DOMAIN = res.DESKTOP_DOMAIN;
    PVC_STORAGE_MAX = res.PVC_STORAGE_MAX;
    GPU_ENABLED = res.GPU_ENABLED;
    
    // Load Istio configuration
    ISTIO_ENABLED = res.ISTIO_ENABLED || false;
    ISTIO_PUBLIC_DOMAINS = res.ISTIO_PUBLIC_DOMAINS || [];
    ISTIO_SHARED_GATEWAY = res.ISTIO_SHARED_GATEWAY || 'sealos-gateway';
    ISTIO_ENABLE_TRACING = res.ISTIO_ENABLE_TRACING || false;

    return {
      SEALOS_DOMAIN,
      DOMAIN_PORT,
      CURRENCY,
      FORM_SLIDER_LIST_CONFIG: res.FORM_SLIDER_LIST_CONFIG,
      DESKTOP_DOMAIN: res.DESKTOP_DOMAIN,
      GPU_ENABLED,
      ISTIO_ENABLED: ISTIO_ENABLED,
      ISTIO_PUBLIC_DOMAINS: ISTIO_PUBLIC_DOMAINS,
      ISTIO_SHARED_GATEWAY: ISTIO_SHARED_GATEWAY,
      ISTIO_ENABLE_TRACING: ISTIO_ENABLE_TRACING
    };
  } catch (error) {}

  return {
    SEALOS_DOMAIN
  };
};

// server side method
export const serverLoadInitData = () => {
  try {
    SEALOS_DOMAIN = global.AppConfig.cloud.domain || 'cloud.sealos.io';
    DOMAIN_PORT = global.AppConfig.cloud.port || '';
    SHOW_EVENT_ANALYZE = global.AppConfig.launchpad.eventAnalyze.enabled;
    SEALOS_USER_DOMAINS = global.AppConfig.cloud.userDomains;
  } catch (error) {}
};
