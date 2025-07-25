import { NextRequest } from 'next/server';

import { authSession } from '@/services/backend/auth';
import { getK8s } from '@/services/backend/kubernetes';
import { jsonRes } from '@/services/backend/response';
import { devboxDB } from '@/services/db/init';
import { KBDevboxTypeV2 } from '@/types/k8s';
import { json2DevboxV2, json2Service, generateNetworkingYaml } from '@/utils/json2Yaml';
import { RequestSchema } from './schema';

export const dynamic = 'force-dynamic';

export async function POST(req: NextRequest) {
  try {
    const body = await req.json();
    const validationResult = RequestSchema.safeParse(body);

    if (!validationResult.success) {
      return jsonRes({
        code: 400,
        message: 'Invalid request body',
        error: validationResult.error.errors
      });
    }

    const devboxForm = validationResult.data;
    const headerList = req.headers;

    const { applyYamlList, k8sCustomObjects, namespace } = await getK8s({
      kubeconfig: await authSession(headerList)
    });

    const { body: devboxListBody } = (await k8sCustomObjects.listNamespacedCustomObject(
      'devbox.sealos.io',
      'v1alpha1',
      namespace,
      'devboxes'
    )) as {
      body: {
        items: KBDevboxTypeV2[];
      };
    };

    if (
      !!devboxListBody &&
      devboxListBody.items.length > 0 &&
      devboxListBody.items.find((item) => item.metadata.name === devboxForm.name)
    ) {
      return jsonRes({
        code: 409,
        message: 'Devbox already exists'
      });
    }

    const template = await devboxDB.template.findUnique({
      where: {
        uid: devboxForm.templateUid,
        isDeleted: false
      }
    });

    if (!template) {
      return jsonRes({
        code: 404,
        message: 'Template not found'
      });
    }

    const { INGRESS_SECRET, DEVBOX_AFFINITY_ENABLE, SQUASH_ENABLE, ISTIO_ENABLED, USE_ISTIO, ISTIO_SHARED_GATEWAY } = process.env;
    const devbox = json2DevboxV2(devboxForm, DEVBOX_AFFINITY_ENABLE, SQUASH_ENABLE);
    const service = json2Service(devboxForm);
    
    // Use Istio or Ingress based on configuration
    const useIstio = ISTIO_ENABLED === 'true' || USE_ISTIO === 'true';
    const networkingMode = useIstio ? 'istio' : 'ingress';
    const networkingYaml = generateNetworkingYaml(devboxForm, networkingMode, {
      ingressSecret: INGRESS_SECRET,
      sharedGateway: useIstio && ISTIO_SHARED_GATEWAY ? true : false,
      gatewayName: ISTIO_SHARED_GATEWAY || 'sealos-gateway'
    });
    
    await applyYamlList([devbox, service, networkingYaml].filter(yaml => yaml), 'create');

    return jsonRes({
      data: 'success create devbox'
    });
  } catch (err: any) {
    return jsonRes({
      code: 500,
      message: err?.message || 'Internal server error',
      error: err
    });
  }
}
