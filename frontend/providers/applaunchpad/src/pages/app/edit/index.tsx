import { postDeployApp, putApp } from '@/api/app';
import { checkPermission } from '@/api/platform';
import { defaultSliderKey } from '@/constants/app';
import { defaultEditVal, editModeMap } from '@/constants/editApp';
import { useConfirm } from '@/hooks/useConfirm';
import { useLoading } from '@/hooks/useLoading';
import { useAppStore } from '@/store/app';
import { useGlobalStore } from '@/store/global';
import { useUserStore } from '@/store/user';
import type { YamlItemType } from '@/types';
import type { AppEditSyncedFields, AppEditType, DeployKindsType } from '@/types/app';
import { adaptEditAppData } from '@/utils/adapt';
import {
  json2ConfigMap,
  json2DeployCr,
  json2HPA,
  json2Ingress,
  json2Secret,
  json2Service,
  generateNetworkingResources,
  getNetworkingMode
} from '@/utils/deployYaml2Json';
import { ISTIO_ENABLED, ISTIO_ENABLE_TRACING } from '@/store/static';
import { serviceSideProps } from '@/utils/i18n';
import { patchYamlList } from '@/utils/tools';
import { Box, Flex } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'next-i18next';
import dynamic from 'next/dynamic';
import { useRouter } from 'next/router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import Form from './components/Form';
import Header from './components/Header';
import Yaml from './components/Yaml';
import { useMessage } from '@sealos/ui';
import { customAlphabet } from 'nanoid';
import { ResponseCode } from '@/types/response';
import { useGuideStore } from '@/store/guide';

const nanoid = customAlphabet('abcdefghijklmnopqrstuvwxyz', 12);

const ErrorModal = dynamic(() => import('./components/ErrorModal'));

// Helper function to create domain checker with istioConfig
const createDomainChecker = (istioPublicDomains: string[]) => {
  return (domain: string): boolean => {
    console.log('isPublicDomain check:', {
      domain,
      istioPublicDomains,
      hasPublicDomains: istioPublicDomains?.length > 0
    });
    
    if (!istioPublicDomains || istioPublicDomains.length === 0) {
      console.warn('No ISTIO_PUBLIC_DOMAINS configured');
      return false;
    }
    
    // Check if the domain matches any public domain patterns
    const isPublic = istioPublicDomains.some(publicDomain => {
      // Handle wildcard domains (e.g., *.cloud.sealos.io)
      if (publicDomain.startsWith('*.')) {
        const baseDomain = publicDomain.substring(2);
        const matches = domain === baseDomain || domain.endsWith(`.${baseDomain}`);
        if (matches) {
          console.log(`Domain ${domain} matches wildcard pattern ${publicDomain}`);
        }
        return matches;
      }
      // Exact match
      const exactMatch = domain === publicDomain;
      if (exactMatch) {
        console.log(`Domain ${domain} exactly matches ${publicDomain}`);
      }
      return exactMatch;
    });
    
    console.log(`Domain ${domain} is ${isPublic ? 'PUBLIC' : 'CUSTOM'}`);
    return isPublic;
  };
};

// Helper function to create gateway options getter with istioConfig
const createGatewayOptionsGetter = (istioConfig: {
  enabled: boolean;
  publicDomains: string[];
  sharedGateway: string;
  enableTracing: boolean;
}) => {
  const isPublicDomain = createDomainChecker(istioConfig.publicDomains);
  
  return (data: AppEditType) => {
    console.log('getGatewayOptions called with:', {
      networks: data.networks,
      istioConfig
    });
    
    if (!istioConfig.enabled) {
      console.log('Istio not enabled, using ingress mode');
      return {
        networkingMode: 'ingress' as const,
        enableSmartGateway: false
      };
    }

    // Check if any network uses custom domains
    const hasCustomDomains = data.networks?.some(network => 
      network.openPublicDomain && network.customDomain
    );
    
    console.log('hasCustomDomains:', hasCustomDomains);
    
    // Check if all networks use public domains
    const allPublicDomains = data.networks?.every(network => {
      if (!network.openPublicDomain) return true; // Skip non-public networks
      
      const fullDomain = network.customDomain || `${network.publicDomain}.${network.domain}`;
      console.log('Checking network:', {
        publicDomain: network.publicDomain,
        customDomain: network.customDomain,
        domain: network.domain,
        fullDomain
      });
      return isPublicDomain(fullDomain);
    });

    console.log('allPublicDomains:', allPublicDomains);

    const result = {
      networkingMode: 'istio' as const,
      enableSmartGateway: true,
      // Use shared gateway only if all domains are public
      useSharedGateway: !hasCustomDomains && allPublicDomains,
      sharedGatewayName: istioConfig.sharedGateway
    };
    
    console.log('Gateway options result:', result);
    return result;
  };
};

export const formData2Yamls = (
  data: AppEditType,
  options?: {
    networkingMode?: 'ingress' | 'istio';
    enableSmartGateway?: boolean;
    istioConfig?: {
      enabled: boolean;
      publicDomains: string[];
      sharedGateway: string;
      enableTracing: boolean;
    };
  }
) => {
  // Determine networking mode from runtime configuration or options
  const networkingMode = options?.networkingMode || getNetworkingMode({
    useIstio: ISTIO_ENABLED,
    enableIstio: ISTIO_ENABLED,
    istioEnabled: ISTIO_ENABLED
  });

  const hasPublicNetworks = data.networks.find((item) => item.openPublicDomain);
  
  // Generate networking resources based on mode
  const getNetworkingResources = () => {
    if (!hasPublicNetworks) return [];

    if (networkingMode === 'istio') {
      // Use intelligent Gateway optimization
      const getGatewayOptions = options?.istioConfig 
        ? createGatewayOptionsGetter(options.istioConfig)
        : createGatewayOptionsGetter({
            enabled: ISTIO_ENABLED,
            publicDomains: [],
            sharedGateway: 'sealos-gateway',
            enableTracing: false
          });
      const gatewayOptions = getGatewayOptions(data);
      const istioResources = generateNetworkingResources(data, 'istio', {
        sharedGateway: gatewayOptions.networkingMode === 'istio' ? gatewayOptions.useSharedGateway : false,
        sharedGatewayName: gatewayOptions.networkingMode === 'istio' ? gatewayOptions.sharedGatewayName : 'sealos-gateway',
        enableTracing: options?.istioConfig?.enableTracing || ISTIO_ENABLE_TRACING
      });

      if (istioResources) {
        return [{
          filename: 'istio-networking.yaml',
          value: istioResources
        }];
      }
    } else {
      // Fallback to traditional Ingress
      const ingressYaml = json2Ingress(data);
      if (ingressYaml) {
        return [{
          filename: 'ingress.yaml',
          value: ingressYaml
        }];
      }
    }

    return [];
  };

  return [
    {
      filename: 'service.yaml',
      value: json2Service(data)
    },
    data.kind === 'statefulset' || data.storeList?.length > 0
      ? {
          filename: 'statefulset.yaml',
          value: json2DeployCr(data, 'statefulset')
        }
      : {
          filename: 'deployment.yaml',
          value: json2DeployCr(data, 'deployment')
        },
    ...(data.configMapList.length > 0
      ? [
          {
            filename: 'configmap.yaml',
            value: json2ConfigMap(data)
          }
        ]
      : []),
    // Use smart networking resources generation
    ...getNetworkingResources(),
    ...(data.hpa.use
      ? [
          {
            filename: 'hpa.yaml',
            value: json2HPA(data)
          }
        ]
      : []),
    ...(data.secret.use
      ? [
          {
            filename: 'secret.yaml',
            value: json2Secret(data)
          }
        ]
      : [])
  ];
};

const EditApp = ({ appName, tabType }: { appName?: string; tabType: string }) => {
  const { t } = useTranslation();
  const formOldYamls = useRef<YamlItemType[]>([]);
  const crOldYamls = useRef<DeployKindsType[]>([]);
  const oldAppEditData = useRef<AppEditType>();
  const { message: toast } = useMessage();
  const { Loading, setIsLoading } = useLoading();
  const router = useRouter();
  const [forceUpdate, setForceUpdate] = useState(false);
  const { setAppDetail } = useAppStore();
  const { screenWidth, formSliderListConfig, istioConfig } = useGlobalStore();
  const { userSourcePrice, loadUserSourcePrice } = useUserStore();
  const { title, applyBtnText, applyMessage, applySuccess, applyError } = editModeMap(!!appName);
  const [yamlList, setYamlList] = useState<YamlItemType[]>([]);
  const [errorMessage, setErrorMessage] = useState('');
  const [errorCode, setErrorCode] = useState<ResponseCode>();
  const [already, setAlready] = useState(false);
  const [isAdvancedOpen] = useState(false);
  const [defaultStorePathList, setDefaultStorePathList] = useState<string[]>([]); // default store will no be edit
  const [defaultGpuSource, setDefaultGpuSource] = useState<AppEditType['gpu']>({
    type: '',
    amount: 0,
    manufacturers: ''
  });
  const { openConfirm, ConfirmChild } = useConfirm({
    content: applyMessage
  });
  const pxVal = useMemo(() => {
    const val = Math.floor((screenWidth - 1050) / 2);
    if (val < 20) {
      return 20;
    }
    return val;
  }, [screenWidth]);
  const { createCompleted } = useGuideStore();

  // form
  const formHook = useForm<AppEditType>({
    defaultValues: defaultEditVal
  });

  const realTimeForm = useRef(defaultEditVal);

  // watch form change, compute new yaml
  formHook.watch((data) => {
    if (!data) return;
    realTimeForm.current = data as AppEditType;
    setForceUpdate(!forceUpdate);
  });

  const { refetch: refetchPrice } = useQuery(['init-price'], loadUserSourcePrice, {
    enabled: !!userSourcePrice?.gpu,
    refetchInterval: 6000
  });

  // add already deployment gpu amount if they exists
  const countGpuInventory = useCallback(
    (type?: string) => {
      const inventory = userSourcePrice?.gpu?.find((item) => item.type === type)?.inventory || 0;
      const defaultInventory = type === defaultGpuSource?.type ? defaultGpuSource?.amount || 0 : 0;
      return inventory + defaultInventory;
    },
    [defaultGpuSource?.amount, defaultGpuSource?.type, userSourcePrice?.gpu]
  );

  const submitSuccess = useCallback(
    async (yamlList: YamlItemType[]) => {
      if (!createCompleted) {
        return router.push('/app/detail?name=hello&guide=true');
      }

      setIsLoading(true);
      try {
        const parsedNewYamlList = yamlList.map((item) => item.value);

        if (appName) {
          const patch = patchYamlList({
            parsedOldYamlList: formOldYamls.current.map((item) => item.value),
            parsedNewYamlList: parsedNewYamlList,
            originalYamlList: crOldYamls.current
          });
          await putApp({
            patch,
            appName,
            stateFulSetYaml: yamlList.find((item) => item.filename === 'statefulset.yaml')?.value
          });
        } else {
          await postDeployApp(parsedNewYamlList);
        }

        router.replace(`/app/detail?name=${formHook.getValues('appName')}`);

        toast({
          title: t(applySuccess),
          status: 'success'
        });

        if (userSourcePrice?.gpu) {
          refetchPrice();
        }
      } catch (error: any) {
        if (error?.code === ResponseCode.BALANCE_NOT_ENOUGH) {
          setErrorMessage(t('user_balance_not_enough'));
          setErrorCode(ResponseCode.BALANCE_NOT_ENOUGH);
        } else if (error?.code === ResponseCode.FORBIDDEN_CREATE_APP) {
          setErrorMessage(t('forbidden_create_app'));
          setErrorCode(ResponseCode.FORBIDDEN_CREATE_APP);
        } else if (error?.code === ResponseCode.APP_ALREADY_EXISTS) {
          setErrorMessage(t('app_already_exists'));
          setErrorCode(ResponseCode.APP_ALREADY_EXISTS);
        } else {
          setErrorMessage(JSON.stringify(error));
        }
      }
      setIsLoading(false);
    },
    [
      setIsLoading,
      toast,
      appName,
      router,
      formHook,
      t,
      applySuccess,
      userSourcePrice?.gpu,
      refetchPrice,
      createCompleted
    ]
  );

  const submitError = useCallback(() => {
    // deep search message
    const deepSearch = (obj: any): string => {
      if (!obj || typeof obj !== 'object') return t('Submit Error');
      if (!!obj.message) {
        return obj.message;
      }
      return deepSearch(Object.values(obj)[0]);
    };
    toast({
      title: deepSearch(formHook.formState.errors),
      status: 'error',
      position: 'top',
      duration: 3000,
      isClosable: true
    });
  }, [formHook.formState.errors, t, toast]);

  useQuery(
    ['initLaunchpadApp'],
    () => {
      if (!appName) {
        const defaultApp = {
          ...defaultEditVal,
          cpu: formSliderListConfig[defaultSliderKey].cpu[0],
          memory: formSliderListConfig[defaultSliderKey].memory[0]
        };
        setAlready(true);
        setYamlList([
          {
            filename: 'service.yaml',
            value: json2Service(defaultApp)
          },
          {
            filename: 'deployment.yaml',
            value: json2DeployCr(defaultApp, 'deployment')
          }
        ]);
        return null;
      }
      setIsLoading(true);
      refetchPrice();
      return setAppDetail(appName);
    },
    {
      onSuccess(res) {
        if (!res) return;
        console.log(res, 'init res');
        oldAppEditData.current = res;
        const initialGatewayOptions = createGatewayOptionsGetter(istioConfig)(res);
        formOldYamls.current = formData2Yamls(res, {
          ...initialGatewayOptions,
          istioConfig
        });
        crOldYamls.current = res.crYamlList;

        setDefaultStorePathList(res.storeList.map((item) => item.path));
        setDefaultGpuSource(res.gpu);
        formHook.reset(adaptEditAppData(res));
        setAlready(true);
        const currentGatewayOptions = createGatewayOptionsGetter(istioConfig)(realTimeForm.current);
        setYamlList(formData2Yamls(realTimeForm.current, {
          ...currentGatewayOptions,
          istioConfig
        }));
      },
      onError(err) {
        toast({
          title: String(err),
          status: 'error'
        });
      },
      onSettled() {
        setIsLoading(false);
      }
    }
  );

  useEffect(() => {
    if (tabType === 'yaml') {
      try {
        const gatewayOptions = createGatewayOptionsGetter(istioConfig)(realTimeForm.current);
        setYamlList(formData2Yamls(realTimeForm.current, {
          ...gatewayOptions,
          istioConfig
        }));
      } catch (error) {}
    }
  }, [router.query.name, tabType]);

  useEffect(() => {
    try {
      console.log('edit page already', already, router.query);
      if (!already) return;
      const query = router.query as { formData?: string; name?: string };
      if (!query.formData) return;

      const parsedData: Partial<AppEditSyncedFields> = JSON.parse(
        decodeURIComponent(query.formData)
      );

      const basicFields: (keyof AppEditSyncedFields)[] = router.query?.name
        ? ['imageName', 'cpu', 'memory']
        : ['imageName', 'replicas', 'cpu', 'memory', 'cmdParam', 'runCMD', 'appName', 'labels'];

      basicFields.forEach((field) => {
        if (parsedData[field] !== undefined) {
          formHook.setValue(field, parsedData[field] as any);
        }
      });

      if (Array.isArray(parsedData.networks)) {
        const completeNetworks = parsedData.networks.map((network) => ({
          networkName: network.networkName || `network-${nanoid()}`,
          portName: network.portName || nanoid(),
          port: network.port || 80,
          protocol: network.protocol || 'TCP',
          appProtocol: network.appProtocol || 'HTTP',
          openPublicDomain: network.openPublicDomain || false,
          openNodePort: network.openNodePort || false,
          publicDomain: network.publicDomain || nanoid(),
          customDomain: network.customDomain || '',
          domain: network.domain || 'gzg.sealos.run'
        }));
        formHook.setValue('networks', completeNetworks);
      }
    } catch (error) {}
  }, [router.query, already]);

  return (
    <>
      <Flex
        flexDirection={'column'}
        alignItems={'center'}
        h={'100%'}
        minWidth={'1024px'}
        backgroundColor={'grayModern.100'}
        overflowY={'auto'}
      >
        <Header
          appName={formHook.getValues('appName')}
          title={title}
          yamlList={yamlList}
          applyBtnText={applyBtnText}
          applyCb={() => {
            formHook.handleSubmit(async (data) => {
              const gatewayOptions = createGatewayOptionsGetter(istioConfig)(data);
              const parseYamls = formData2Yamls(data, {
                ...gatewayOptions,
                istioConfig
              });
              setYamlList(parseYamls);

              // gpu inventory check
              if (data.gpu?.type) {
                const inventory = countGpuInventory(data.gpu?.type);
                if (data.gpu?.amount > inventory) {
                  return toast({
                    status: 'warning',
                    title: t('Gpu under inventory Tip', {
                      gputype: data.gpu.type
                    })
                  });
                }
              }
              // quote check
              // const quoteCheckRes = checkQuotaAllow(data, oldAppEditData.current);
              // if (quoteCheckRes) {
              //   return toast({
              //     status: 'warning',
              //     title: t(quoteCheckRes),
              //     duration: 5000,
              //     isClosable: true
              //   });
              // }

              // check network port
              if (!checkNetworkPorts(data.networks)) {
                return toast({
                  status: 'warning',
                  title: t('Network port conflict')
                });
              }

              // check permission
              if (appName) {
                try {
                  const result = await checkPermission({
                    appName: data.appName
                  });
                  if (result === 'insufficient_funds') {
                    return toast({
                      status: 'warning',
                      title: t('user.Insufficient account balance')
                    });
                  }
                } catch (error: any) {
                  console.error('Permission check failed:', error);
                  
                  // Handle authentication errors
                  if (error?.code === 401 || error?.message?.includes('unAuthorization')) {
                    return toast({
                      status: 'error',
                      title: t('Authentication failed'),
                      description: t('Please refresh the page and login again')
                    });
                  }
                  
                  return toast({
                    status: 'warning',
                    title: error?.message || 'Check Error'
                  });
                }
              }

              openConfirm(() => submitSuccess(parseYamls))();
            }, submitError)();
          }}
        />

        <Box flex={'1 0 0'} h={0} w={'100%'} pb={4}>
          {tabType === 'form' ? (
            <Form
              formHook={formHook}
              already={already}
              defaultStorePathList={defaultStorePathList}
              countGpuInventory={countGpuInventory}
              pxVal={pxVal}
              refresh={forceUpdate}
              isAdvancedOpen={isAdvancedOpen}
            />
          ) : (
            <Yaml yamlList={yamlList} pxVal={pxVal} />
          )}
        </Box>
      </Flex>
      <ConfirmChild />
      <Loading />
      {!!errorMessage && (
        <ErrorModal
          title={applyError}
          content={errorMessage}
          onClose={() => setErrorMessage('')}
          errorCode={errorCode}
        />
      )}
    </>
  );
};

export async function getServerSideProps(content: any) {
  const appName = content?.query?.name || '';
  const tabType = content?.query?.type || 'form';

  return {
    props: {
      appName,
      tabType,
      ...(await serviceSideProps(content))
    }
  };
}

export default EditApp;

function checkNetworkPorts(networks: AppEditType['networks']) {
  const portProtocolSet = new Set<string>();

  for (const network of networks) {
    const { port, protocol } = network;
    const key = `${port}-${protocol}`;
    if (portProtocolSet.has(key)) {
      return false;
    }
    portProtocolSet.add(key);
  }

  return true;
}
