import yaml from 'js-yaml';

import { devboxKey, publicDomainKey } from '@/constants/devbox';
import { DevboxEditTypeV2, ProtocolType } from '@/types/devbox';
import { str2Num } from './tools';

export const json2Gateway = (
  data: Pick<DevboxEditTypeV2, 'name' | 'networks'>,
  gatewayName?: string
) => {
  const openNetworks = data.networks.filter((item) => item.openPublicDomain);
  
  if (openNetworks.length === 0) {
    return '';
  }

  const allHosts = openNetworks.map((network) => 
    network.customDomain ? network.customDomain : network.publicDomain
  );

  const gateway = {
    apiVersion: 'networking.istio.io/v1beta1',
    kind: 'Gateway',
    metadata: {
      name: gatewayName || `${data.name}-gateway`,
      labels: {
        [devboxKey]: data.name
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
            credentialName: 'wildcard-cert' // Default TLS secret
          },
          hosts: allHosts
        }
      ]
    }
  };

  return yaml.dump(gateway);
};

export const json2VirtualService = (
  data: Pick<DevboxEditTypeV2, 'name' | 'networks'>,
  gatewayName?: string
) => {
  const openNetworks = data.networks.filter((item) => item.openPublicDomain);
  
  if (openNetworks.length === 0) {
    return '';
  }

  const result = openNetworks.map((network, i) => {
    const host = network.customDomain ? network.customDomain : network.publicDomain;
    const protocol = network.protocol as ProtocolType;

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

    // Build timeout configuration
    const getTimeout = () => {
      switch (protocol) {
        case 'WS':
          return '0s'; // No timeout for WebSocket
        case 'GRPC':
          return '0s'; // No timeout for gRPC streaming
        default:
          return '300s';
      }
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
        allowCredentials: false
      };
    };

    const virtualService = {
      apiVersion: 'networking.istio.io/v1beta1',
      kind: 'VirtualService',
      metadata: {
        name: network.networkName,
        labels: {
          [devboxKey]: data.name,
          [publicDomainKey]: network.publicDomain
        }
      },
      spec: {
        hosts: [host],
        gateways: [gatewayName || `${data.name}-gateway`],
        http: [
          {
            match: buildMatchRules(),
            route: [
              {
                destination: {
                  host: data.name,
                  port: {
                    number: network.port
                  }
                }
              }
            ],
            timeout: getTimeout(),
            corsPolicy: getCorsPolicy(),
            ...(protocol === 'WS' ? {
              headers: {
                request: {
                  set: {
                    'X-Forwarded-Proto': 'https'
                  }
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
  data: Pick<DevboxEditTypeV2, 'name' | 'networks'>,
  options?: {
    gatewayName?: string;
    sharedGateway?: boolean;
  }
) => {
  const { gatewayName, sharedGateway = false } = options || {};
  
  const openNetworks = data.networks.filter((item) => item.openPublicDomain);
  
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
  const virtualServiceYaml = json2VirtualService(data, gatewayName);
  if (virtualServiceYaml) {
    results.push(virtualServiceYaml);
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
          [devboxKey]: data.name
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
          [devboxKey]: data.name
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

// Helper function to generate YAML list with Istio resources
export const generateIstioYamlList = (
  data: DevboxEditTypeV2,
  env: {
    devboxAffinityEnable?: string;
    squashEnable?: string;
    useIstio?: boolean;
    sharedGateway?: boolean;
    gatewayName?: string;
  }
) => {
  const baseYamls = [
    {
      filename: 'devbox.yaml',
      value: '' // This would be filled by existing json2DevboxV2 function
    }
  ];

  if (data.networks.length > 0) {
    baseYamls.push({
      filename: 'service.yaml',
      value: '' // This would be filled by existing json2Service function
    });
  }

  // Add networking resources based on configuration
  if (data.networks.find((item) => item.openPublicDomain)) {
    if (env.useIstio) {
      baseYamls.push({
        filename: 'istio-networking.yaml',
        value: json2IstioResources(data, {
          gatewayName: env.gatewayName,
          sharedGateway: env.sharedGateway
        })
      });
    } else {
      baseYamls.push({
        filename: 'ingress.yaml',
        value: '' // This would be filled by existing json2Ingress function
      });
    }
  }

  return baseYamls.filter(item => item.value); // Remove empty entries
};