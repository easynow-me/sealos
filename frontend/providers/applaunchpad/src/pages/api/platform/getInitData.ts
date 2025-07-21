import { Coin } from '@/constants/app';
import { jsonRes } from '@/services/backend/response';
import type { AppConfigType, EnvResponse } from '@/types';
import { readFileSync } from 'fs';
import * as yaml from 'js-yaml';
import type { NextApiRequest, NextApiResponse } from 'next';
import { getGpuNode } from './resourcePrice';

export const defaultAppConfig: AppConfigType = {
  cloud: {
    domain: 'cloud.sealos.io',
    port: '',
    userDomains: [
      {
        name: 'cloud.sealos.io',
        secretName: 'wildcard-cert'
      }
    ],
    desktopDomain: 'cloud.sealos.io'
  },
  common: {
    guideEnabled: false,
    apiEnabled: false,
    gpuEnabled: false
  },
  istio: {
    enabled: false,
    publicDomains: [],
    sharedGateway: 'sealos-gateway',
    enableTracing: false
  },
  launchpad: {
    meta: {
      title: 'Sealos Desktop App Demo',
      description: 'Sealos Desktop App Demo',
      scripts: []
    },
    currencySymbol: Coin.shellCoin,
    pvcStorageMax: 20,
    eventAnalyze: {
      enabled: false,
      fastGPTKey: ''
    },
    components: {
      monitor: {
        url: 'http://launchpad-monitor.sealos.svc.cluster.local:8428'
      },
      billing: {
        url: 'http://account-service.account-system.svc:2333'
      },
      log: {
        url: 'http://localhost:8080'
      }
    },
    appResourceFormSliderConfig: {
      default: {
        cpu: [100, 200, 500, 1000, 2000, 3000, 4000, 8000],
        memory: [64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384]
      }
    },
    fileManger: {
      uploadLimit: 5,
      downloadLimit: 100
    }
  }
};

process.on('unhandledRejection', (reason, promise) => {
  console.error(`Caught unhandledRejection:`, reason, promise);
});

process.on('uncaughtException', (err) => {
  console.error(`Caught uncaughtException:`, err);
});

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  try {
    if (!global.AppConfig) {
      const filename =
        process.env.NODE_ENV === 'development' ? 'data/config.yaml.local' : '/app/data/config.yaml';
      
      let fileContent: any = {};
      try {
        const fileData = readFileSync(filename, 'utf-8');
        fileContent = yaml.load(fileData) || {};
      } catch (fileError) {
        console.log('Config file not found or empty, using defaults:', filename);
      }
      
      // Deep merge configuration with defaults
      const config: AppConfigType = {
        ...defaultAppConfig,
        ...fileContent,
        // Ensure nested objects are properly merged
        common: {
          ...defaultAppConfig.common,
          ...(fileContent?.common || {})
        },
        launchpad: {
          ...defaultAppConfig.launchpad,
          ...(fileContent?.launchpad || {}),
          // Deep merge nested launchpad properties
          meta: {
            ...defaultAppConfig.launchpad.meta,
            ...(fileContent?.launchpad?.meta || {})
          },
          eventAnalyze: {
            ...defaultAppConfig.launchpad.eventAnalyze,
            ...(fileContent?.launchpad?.eventAnalyze || {})
          },
          components: {
            ...defaultAppConfig.launchpad.components,
            ...(fileContent?.launchpad?.components || {})
          },
          appResourceFormSliderConfig: fileContent?.launchpad?.appResourceFormSliderConfig || defaultAppConfig.launchpad.appResourceFormSliderConfig,
          fileManger: {
            ...defaultAppConfig.launchpad.fileManger,
            ...(fileContent?.launchpad?.fileManger || {})
          }
        },
        cloud: {
          ...defaultAppConfig.cloud,
          ...(fileContent?.cloud || {})
        },
        istio: {
          ...defaultAppConfig.istio,
          ...(fileContent?.istio || {})
        }
      };
      
      global.AppConfig = config;
      
      try {
        const gpuNodes = await getGpuNode();
        console.log(gpuNodes, 'gpuNodes');
        global.AppConfig.common.gpuEnabled = gpuNodes.length > 0;
      } catch (gpuError) {
        console.log('Error getting GPU nodes:', gpuError);
        global.AppConfig.common.gpuEnabled = false;
      }
    }

    // Ensure global.AppConfig exists before passing to getServerEnv
    if (!global.AppConfig) {
      global.AppConfig = defaultAppConfig;
    }

    jsonRes<EnvResponse>(res, {
      data: getServerEnv(global.AppConfig)
    });
  } catch (error) {
    console.log('error: /api/platform/getInitData', error);
    // Return default config on error
    jsonRes<EnvResponse>(res, {
      data: getServerEnv(defaultAppConfig)
    });
  }
}

export const getServerEnv = (AppConfig: AppConfigType): EnvResponse => {
  // Ensure AppConfig has all required properties
  const safeConfig = {
    ...defaultAppConfig,
    ...AppConfig,
    cloud: { ...defaultAppConfig.cloud, ...(AppConfig?.cloud || {}) },
    common: { ...defaultAppConfig.common, ...(AppConfig?.common || {}) },
    launchpad: { ...defaultAppConfig.launchpad, ...(AppConfig?.launchpad || {}) },
    istio: { ...defaultAppConfig.istio, ...(AppConfig?.istio || {}) }
  };

  return {
    SEALOS_DOMAIN: safeConfig.cloud.domain,
    DOMAIN_PORT: safeConfig.cloud.port?.toString() || '',
    SHOW_EVENT_ANALYZE: safeConfig.launchpad.eventAnalyze.enabled,
    FORM_SLIDER_LIST_CONFIG: safeConfig.launchpad.appResourceFormSliderConfig,
    guideEnabled: safeConfig.common.guideEnabled,
    fileMangerConfig: safeConfig.launchpad.fileManger,
    CURRENCY: safeConfig.launchpad.currencySymbol || Coin.shellCoin,
    SEALOS_USER_DOMAINS: safeConfig.cloud.userDomains || [],
    DESKTOP_DOMAIN: safeConfig.cloud.desktopDomain,
    PVC_STORAGE_MAX: safeConfig.launchpad.pvcStorageMax || 20,
    GPU_ENABLED: safeConfig.common.gpuEnabled || false,
    // Istio configuration
    ISTIO_ENABLED: safeConfig.istio?.enabled || false,
    ISTIO_PUBLIC_DOMAINS: safeConfig.istio?.publicDomains || [],
    ISTIO_SHARED_GATEWAY: safeConfig.istio?.sharedGateway || 'sealos-gateway',
    ISTIO_ENABLE_TRACING: safeConfig.istio?.enableTracing || false
  };
};
