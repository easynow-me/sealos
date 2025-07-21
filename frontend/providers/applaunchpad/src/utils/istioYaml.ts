import {
  appDeployKey,
  publicDomainKey
} from '@/constants/app';
import { SEALOS_USER_DOMAINS } from '@/store/static';
import type { AppEditType } from '@/types/app';
import yaml from 'js-yaml';

export const json2Gateway = (data: AppEditType, gatewayName?: string) => {
  const openNetworks = data.networks.filter((item) => item.openPublicDomain && !item.openNodePort);
  
  if (openNetworks.length === 0) {
    return '';
  }

  const allHosts = openNetworks.map((network) => 
    network.customDomain 
      ? network.customDomain 
      : `${network.publicDomain}.${network.domain}`
  );

  const gateway = {
    apiVersion: 'networking.istio.io/v1beta1',
    kind: 'Gateway',
    metadata: {
      name: gatewayName || `${data.appName}-gateway`,
      labels: {
        [appDeployKey]: data.appName
      }
    },
    spec: {
      selector: {
        istio: 'ingressgateway'
      },
      servers: [
        {
          port: {
            number: 80,
            name: 'http',
            protocol: 'HTTP'
          },
          hosts: allHosts
        },
        {
          port: {
            number: 443,
            name: 'https',
            protocol: 'HTTPS'
          },
          tls: {
            mode: 'SIMPLE',
            credentialName: 'wildcard-cert' // Default, will be customized per domain
          },
          hosts: allHosts
        }
      ]
    }
  };

  return yaml.dump(gateway);
};

export const json2VirtualService = (data: AppEditType, gatewayName?: string, options?: { publicDomains?: string[] }) => {
  const openNetworks = data.networks.filter((item) => item.openPublicDomain && !item.openNodePort);
  
  if (openNetworks.length === 0) {
    return '';
  }

  const result = openNetworks.map((network, i) => {
    const host = network.customDomain
      ? network.customDomain
      : `${network.publicDomain}.${network.domain}`;

    const protocol = network.appProtocol;

    // Build match rules based on protocol
    const buildMatchRules = () => {
      const baseMatch = {
        uri: {
          prefix: '/'
        }
      };

      switch (protocol) {
        case 'WS':
          return [{
            ...baseMatch,
            headers: {
              upgrade: {
                exact: 'websocket'
              }
            }
          }];
        case 'GRPC':
          return [{
            ...baseMatch,
            headers: {
              'content-type': {
                prefix: 'application/grpc'
              }
            }
          }];
        default:
          return [baseMatch];
      }
    };

    // Build timeout configuration based on protocol
    const getTimeout = () => {
      switch (protocol) {
        case 'WS':
          return '0s'; // No timeout for WebSocket
        case 'GRPC':
          return '0s'; // No timeout for gRPC streaming
        default:
          return '300s'; // 5 minutes for HTTP
      }
    };

    // Build retry configuration
    const getRetryPolicy = () => {
      if (protocol === 'WS' || protocol === 'GRPC') {
        return undefined; // No retries for streaming protocols
      }
      return {
        attempts: 3,
        perTryTimeout: '30s',
        retryOn: 'gateway-error,connect-failure,refused-stream'
      };
    };

    // Build CORS policy
    const getCorsPolicy = () => {
      return {
        allowOrigins: [
          {
            regex: '.*'
          }
        ],
        allowMethods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS', 'PATCH'],
        allowHeaders: ['content-type', 'authorization', 'upgrade', 'connection'],
        allowCredentials: false,
        maxAge: '24h'
      };
    };

    // Build custom headers for specific protocols
    const getHeaders = () => {
      const headers: any = {
        request: {
          set: {
            'X-Forwarded-Proto': 'https'
          }
        }
      };

      if (protocol === 'WS') {
        headers.request.set['Connection'] = 'upgrade';
        headers.request.set['Upgrade'] = 'websocket';
      }

      return headers;
    };

    const virtualService = {
      apiVersion: 'networking.istio.io/v1beta1',
      kind: 'VirtualService',
      metadata: {
        name: network.networkName,
        labels: {
          [appDeployKey]: data.appName,
          [publicDomainKey]: network.publicDomain
        }
      },
      spec: {
        hosts: [host],
        gateways: [gatewayName || `${data.appName}-gateway`],
        http: [
          {
            match: buildMatchRules(),
            route: [
              {
                destination: {
                  host: data.appName,
                  port: {
                    number: network.port
                  }
                },
                weight: 100
              }
            ],
            timeout: getTimeout(),
            retries: getRetryPolicy(),
            corsPolicy: getCorsPolicy(),
            headers: getHeaders(),
            // Add fault injection for testing if needed
            ...(process.env.NODE_ENV === 'development' ? {
              fault: {
                delay: {
                  percentage: {
                    value: 0.1
                  },
                  fixedDelay: '5s'
                }
              }
            } : {})
          }
        ]
      }
    };

    return yaml.dump(virtualService);
  });

  return result.join('\n---\n');
};

export const json2IstioResources = (
  data: AppEditType,
  options?: {
    gatewayName?: string;
    sharedGateway?: boolean;
    sharedGatewayName?: string;
    enableTracing?: boolean;
    publicDomains?: string[];
  }
) => {
  const { gatewayName, sharedGateway = false, sharedGatewayName, enableTracing = false, publicDomains } = options || {};
  
  const openNetworks = data.networks.filter((item) => item.openPublicDomain && !item.openNodePort);
  
  if (openNetworks.length === 0) {
    return '';
  }

  const results: string[] = [];

  // Add Gateway if not using shared gateway
  if (!sharedGateway) {
    const gatewayYaml = json2Gateway(data, gatewayName);
    if (gatewayYaml) {
      results.push(gatewayYaml);
    }
  }

  // Add VirtualServices
  // Use sharedGatewayName when in shared gateway mode
  const effectiveGatewayName = sharedGateway && sharedGatewayName ? sharedGatewayName : gatewayName;
  const virtualServiceYaml = json2VirtualService(data, effectiveGatewayName, options);
  if (virtualServiceYaml) {
    results.push(virtualServiceYaml);
  }

  // Add DestinationRule for advanced traffic management
  if (enableTracing || data.networks.some(n => n.appProtocol === 'GRPC')) {
    const destinationRule = {
      apiVersion: 'networking.istio.io/v1beta1',
      kind: 'DestinationRule',
      metadata: {
        name: `${data.appName}-destination`,
        labels: {
          [appDeployKey]: data.appName
        }
      },
      spec: {
        host: data.appName,
        trafficPolicy: {
          connectionPool: {
            tcp: {
              maxConnections: 100
            },
            http: {
              http1MaxPendingRequests: 100,
              http2MaxRequests: 1000,
              maxRequestsPerConnection: 2,
              maxRetries: 3,
              idleTimeout: '90s',
              h2UpgradePolicy: 'UPGRADE'
            }
          },
          ...(enableTracing ? {
            outlierDetection: {
              consecutiveErrors: 5,
              interval: '30s',
              baseEjectionTime: '30s',
              maxEjectionPercent: 50
            }
          } : {})
        }
      }
    };

    results.push(yaml.dump(destinationRule));
  }

  // Add custom domain certificates if needed
  const customDomainNetworks = openNetworks.filter(network => network.customDomain);
  
  for (const network of customDomainNetworks) {
    const issuer = {
      apiVersion: 'cert-manager.io/v1',
      kind: 'Issuer',
      metadata: {
        name: network.networkName,
        labels: {
          [appDeployKey]: data.appName
        }
      },
      spec: {
        acme: {
          server: 'https://acme-v02.api.letsencrypt.org/directory',
          email: 'admin@sealos.io',
          privateKeySecretRef: {
            name: 'letsencrypt-prod'
          },
          solvers: [
            {
              http01: {
                ingress: {
                  class: 'nginx',
                  serviceType: 'ClusterIP'
                }
              }
            }
          ]
        }
      }
    };

    const certificate = {
      apiVersion: 'cert-manager.io/v1',
      kind: 'Certificate',
      metadata: {
        name: network.networkName,
        labels: {
          [appDeployKey]: data.appName
        }
      },
      spec: {
        secretName: network.networkName,
        dnsNames: [network.customDomain],
        issuerRef: {
          name: network.networkName,
          kind: 'Issuer'
        }
      }
    };

    results.push(yaml.dump(issuer));
    results.push(yaml.dump(certificate));
  }

  return results.join('\n---\n');
};

// Enhanced version that supports both Ingress and Istio
export const json2NetworkingResources = (
  data: AppEditType,
  mode: 'ingress' | 'istio' = 'ingress',
  options?: {
    gatewayName?: string;
    sharedGateway?: boolean;
    enableTracing?: boolean;
  }
) => {
  if (mode === 'istio') {
    return json2IstioResources(data, options);
  } else {
    // This would call the existing json2Ingress function
    // For now, return empty string as placeholder
    return '';
  }
};

// Migration helper function
export const convertIngressToIstio = (ingressYaml: string): string => {
  try {
    const docs = yaml.loadAll(ingressYaml);
    const istioResources: any[] = [];

    for (const doc of docs) {
      if (doc && typeof doc === 'object' && (doc as any).kind === 'Ingress') {
        const ingress = doc as any;
        
        // Extract basic information
        const name = ingress.metadata.name;
        const labels = ingress.metadata.labels || {};
        const rules = ingress.spec.rules || [];
        const tls = ingress.spec.tls || [];

        // Create Gateway
        const hosts = rules.map((rule: any) => rule.host);
        const gateway = {
          apiVersion: 'networking.istio.io/v1beta1',
          kind: 'Gateway',
          metadata: {
            name: `${name}-gateway`,
            labels
          },
          spec: {
            selector: {
              istio: 'ingressgateway'
            },
            servers: [
              {
                port: {
                  number: 80,
                  name: 'http',
                  protocol: 'HTTP'
                },
                hosts
              },
              ...(tls.length > 0 ? [{
                port: {
                  number: 443,
                  name: 'https',
                  protocol: 'HTTPS'
                },
                tls: {
                  mode: 'SIMPLE',
                  credentialName: tls[0].secretName
                },
                hosts
              }] : [])
            ]
          }
        };

        // Create VirtualService
        const virtualService = {
          apiVersion: 'networking.istio.io/v1beta1',
          kind: 'VirtualService',
          metadata: {
            name,
            labels
          },
          spec: {
            hosts,
            gateways: [`${name}-gateway`],
            http: rules.map((rule: any) => ({
              match: [{
                uri: {
                  prefix: '/'
                }
              }],
              route: rule.http.paths.map((path: any) => ({
                destination: {
                  host: path.backend.service.name,
                  port: {
                    number: path.backend.service.port.number
                  }
                }
              }))
            }))
          }
        };

        istioResources.push(gateway, virtualService);
      }
    }

    return istioResources.map(resource => yaml.dump(resource)).join('\n---\n');
  } catch (error) {
    console.error('Error converting Ingress to Istio:', error);
    return '';
  }
};