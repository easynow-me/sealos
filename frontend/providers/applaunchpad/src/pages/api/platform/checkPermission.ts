import type { NextApiRequest, NextApiResponse } from 'next';
import { ApiResp } from '@/services/kubernet';
import { authSession } from '@/services/backend/auth';
import { getK8s } from '@/services/backend/kubernetes';
import { jsonRes } from '@/services/backend/response';
import * as k8s from '@kubernetes/client-node';

export default async function handler(req: NextApiRequest, res: NextApiResponse<ApiResp>) {
  try {
    const { appName } = req.query as { appName: string };
    console.log('checkPermission - request query:', req.query);
    console.log('checkPermission - appName:', appName);
    
    if (!appName) {
      return jsonRes(res, {
        code: 400,
        error: 'appName is required'
      });
    }

    const { k8sApp, namespace } = await getK8s({
      kubeconfig: await authSession(req.headers)
    });

    const patchBody = {
      metadata: {
        annotations: {
          'update-time': new Date().toISOString()
        }
      }
    };

    const options = {
      headers: { 'Content-type': k8s.PatchUtils.PATCH_FORMAT_STRATEGIC_MERGE_PATCH }
    };

    let hasPermission = false;
    let resourceFound = false;

    // First, try to check if deployment exists and user has permission
    try {
      // Try to get the deployment first
      await k8sApp.readNamespacedDeployment(appName, namespace);
      resourceFound = true;
      
      // If deployment exists, try to patch it
      await k8sApp.patchNamespacedDeployment(
        appName,
        namespace,
        patchBody,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        options
      );
      hasPermission = true;
    } catch (deployErr: any) {
      console.log('Deployment check error:', deployErr?.statusCode || deployErr?.response?.statusCode);
      
      // If it's a 404, deployment doesn't exist, try statefulset
      if (deployErr?.statusCode === 404 || deployErr?.response?.statusCode === 404) {
        // Try statefulset
        try {
          await k8sApp.readNamespacedStatefulSet(appName, namespace);
          resourceFound = true;
          
          await k8sApp.patchNamespacedStatefulSet(
            appName,
            namespace,
            patchBody,
            undefined,
            undefined,
            undefined,
            undefined,
            undefined,
            options
          );
          hasPermission = true;
        } catch (ssErr: any) {
          console.log('StatefulSet check error:', ssErr?.statusCode || ssErr?.response?.statusCode);
          
          // If both don't exist, it's a new app - user has permission
          if (ssErr?.statusCode === 404 || ssErr?.response?.statusCode === 404) {
            hasPermission = true;
          } else {
            throw ssErr;
          }
        }
      } else {
        // Not a 404 error, re-throw
        throw deployErr;
      }
    }

    jsonRes(res, { code: 200, data: 'success' });
  } catch (err: any) {
    console.error('checkPermission error:', err);
    
    // Check for insufficient funds error
    if (err?.body?.code === 403 && err?.body?.message?.includes('40001')) {
      return jsonRes(res, {
        code: 200,
        data: 'insufficient_funds',
        message: err.body.message
      });
    }
    
    // Check for namespace/user permission errors
    if (err?.statusCode === 403 || err?.response?.statusCode === 403) {
      return jsonRes(res, {
        code: 403,
        error: 'Permission denied',
        message: err?.body?.message || err?.message || 'You do not have permission to access this resource'
      });
    }

    jsonRes(res, {
      code: 500,
      error: err?.body || err?.message || 'Internal server error',
      message: err?.body?.message || err?.message
    });
  }
}
