export interface SourcePrice {
  cpu: number;
  memory: number;
  nodeports: number;
  gpu: {
    alias: string;
    type: string;
    price: number;
    inventory: number;
    vm: number;
  }[];
}

export interface Env {
  sealosDomain: string;
  ingressSecret: string;
  registryAddr: string;
  privacyUrl: string;
  devboxAffinityEnable: string;
  squashEnable: string;
  namespace: string;
  rootRuntimeNamespace: string;
  ingressDomain: string;
  currencySymbol: 'shellCoin' | 'cny' | 'usd';
  // Istio configuration
  istioEnabled?: boolean;
  istioPublicDomains?: string[];
  istioSharedGateway?: string;
  istioEnableTracing?: boolean;
}

export interface RuntimeTypeMap {
  id: string;
  label: string;
  gpu: boolean;
}

// RuntimeTypeMap
// {
//   id: 'go'
//   label: 'go'
// }

export interface RuntimeVersionMap {
  [key: string]: {
    id: string;
    label: string;
    defaultPorts: number[];
  }[];
}

// RuntimeVersionMap
// {
//   go: [
//     {
//       id: '1.17'
//       label: 'go-1.17'
//       defaultPorts: [80]
//     }
//   ]
// }

export interface RuntimeNamespaceMap {
  [key: string]: string;
}
