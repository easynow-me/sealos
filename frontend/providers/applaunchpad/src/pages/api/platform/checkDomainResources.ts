import type { NextApiRequest, NextApiResponse } from 'next';
import { jsonRes } from '@/services/backend/response';
import { authSession } from '@/services/backend/auth';
import { getK8s } from '@/services/backend/kubernetes';
import { ISTIO_ENABLED } from '@/store/static';

export type CheckDomainResourcesParams = {
  domain: string;
  networkingMode?: 'ingress' | 'istio';
};

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  try {
    const { domain, networkingMode = ISTIO_ENABLED ? 'istio' : 'ingress' } = req.body as CheckDomainResourcesParams;

    if (!domain) {
      return jsonRes(res, {
        code: 400,
        error: 'Missing required parameter: domain'
      });
    }

    const kubeconfig = await authSession(req.headers);
    const { k8sNetworkingApp, k8sCustomObjects, namespace } = await getK8s({
      kubeconfig: kubeconfig
    });

    let resourceExists = false;
    let resourceType = '';
    let resourceName = '';

    if (networkingMode === 'istio') {
      // Check VirtualService resources
      try {
        const { body: virtualServices } = await k8sCustomObjects.listNamespacedCustomObject(
          'networking.istio.io',
          'v1beta1',
          namespace,
          'virtualservices'
        );

        const vsList = (virtualServices as any).items || [];
        
        // Check if any VirtualService has the domain
        for (const vs of vsList) {
          const hosts = vs.spec?.hosts || [];
          if (hosts.includes(domain)) {
            resourceExists = true;
            resourceType = 'VirtualService';
            resourceName = vs.metadata?.name || '';
            break;
          }
        }

        // If not found in VirtualService, check Gateway resources
        if (!resourceExists) {
          const { body: gateways } = await k8sCustomObjects.listNamespacedCustomObject(
            'networking.istio.io',
            'v1beta1',
            namespace,
            'gateways'
          );

          const gwList = (gateways as any).items || [];
          
          for (const gw of gwList) {
            const servers = gw.spec?.servers || [];
            for (const server of servers) {
              const hosts = server.hosts || [];
              if (hosts.includes(domain) || hosts.includes(`*.${domain}`)) {
                resourceExists = true;
                resourceType = 'Gateway';
                resourceName = gw.metadata?.name || '';
                break;
              }
            }
            if (resourceExists) break;
          }
        }
      } catch (error) {
        console.error('Error checking Istio resources:', error);
      }
    } else {
      // Check Ingress resources
      try {
        const { body: ingresses } = await k8sNetworkingApp.listNamespacedIngress(namespace);
        
        // Check if any Ingress has the domain
        for (const ingress of ingresses.items) {
          const rules = ingress.spec?.rules || [];
          for (const rule of rules) {
            if (rule.host === domain) {
              resourceExists = true;
              resourceType = 'Ingress';
              resourceName = ingress.metadata?.name || '';
              break;
            }
          }
          if (resourceExists) break;
        }
      } catch (error) {
        console.error('Error checking Ingress resources:', error);
      }
    }

    return jsonRes(res, {
      code: 200,
      data: {
        exists: resourceExists,
        resourceType,
        resourceName,
        networkingMode
      }
    });
  } catch (error: any) {
    console.error('Domain check error:', error);
    jsonRes(res, {
      code: 500,
      error: error?.message || 'Failed to check domain resources'
    });
  }
}