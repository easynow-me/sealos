/**
 * Test script to verify Istio configuration
 * This can be used to test if VirtualService/Gateway are generated correctly
 */

import { formData2Yamls } from '@/pages/app/edit/index';
import { AppEditType } from '@/types/app';
import yaml from 'js-yaml';

// Test data
const testAppData: AppEditType = {
  appName: 'test-app',
  imageName: 'nginx:latest',
  runCMD: '',
  cmdParam: '',
  replicas: 1,
  cpu: 100,
  memory: 128,
  networks: [
    {
      networkName: 'test-network',
      portName: 'http',
      port: 80,
      protocol: 'HTTP',
      openPublicDomain: true,
      publicDomain: 'test-app.cloud.sealos.io',
      customDomain: ''
    }
  ],
  envs: [],
  hpa: {
    use: false,
    target: 'cpu',
    value: 50,
    minReplicas: 1,
    maxReplicas: 3
  },
  configMapList: [],
  storeList: [],
  gpu: {
    type: '',
    amount: 0,
    manufacturers: ''
  },
  secret: {
    use: false,
    username: '',
    password: '',
    serverAddress: 'docker.io'
  }
};

// Test with Istio enabled
export function testIstioGeneration() {
  console.log('Testing Istio YAML generation...\n');
  
  // Simulate Istio enabled
  const istioYamls = formData2Yamls(testAppData, {
    networkingMode: 'istio',
    enableSmartGateway: true
  });
  
  console.log('Generated YAMLs with Istio:');
  istioYamls.forEach(item => {
    console.log(`\n=== ${item.filename} ===`);
    const parsed = yaml.loadAll(item.value);
    parsed.forEach(doc => {
      if (doc && typeof doc === 'object' && 'kind' in doc) {
        console.log(`Kind: ${doc.kind}`);
        if (doc.kind === 'VirtualService' || doc.kind === 'Gateway') {
          console.log('✅ Istio resource generated correctly');
        }
      }
    });
  });
  
  // Test with Ingress (traditional mode)
  const ingressYamls = formData2Yamls(testAppData, {
    networkingMode: 'ingress'
  });
  
  console.log('\n\nGenerated YAMLs with Ingress:');
  ingressYamls.forEach(item => {
    if (item.filename === 'ingress.yaml') {
      console.log(`\n=== ${item.filename} ===`);
      const parsed = yaml.loadAll(item.value);
      parsed.forEach(doc => {
        if (doc && typeof doc === 'object' && 'kind' in doc) {
          console.log(`Kind: ${doc.kind}`);
          if (doc.kind === 'Ingress') {
            console.log('✅ Ingress resource generated correctly');
          }
        }
      });
    }
  });
}

// Run test if this file is executed directly
if (require.main === module) {
  testIstioGeneration();
}