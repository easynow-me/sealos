import { generateTerminalTemplate, TerminalStatus } from '@/interfaces/terminal';
import { authSession } from '@/service/auth';
import { ApplyYaml, CRDMeta, GetCRD, GetUserDefaultNameSpace, K8sApi } from '@/service/kubernetes';
import { jsonRes } from '@/service/response';
import type { NextApiRequest, NextApiResponse } from 'next';

export const terminal_meta: CRDMeta = {
  group: 'terminal.sealos.io',
  version: 'v1',
  namespace: 'terminal-app',
  plural: 'terminals'
};

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  try {
    const kubeconfig = await authSession(req.headers);
    const kc = K8sApi(kubeconfig);

    const kube_user = kc.getCurrentUser();

    if (!kube_user) {
      throw new Error('kube_user get failed');
    }

    // Try to get user name from different properties
    const userName = kube_user.name || kube_user.username;
    const userToken = kube_user.token;

    if (!userName || !userToken) {
      throw new Error('kube_user get failed');
    }

    const terminal_name = 'terminal-' + userName;
    const namespace = GetUserDefaultNameSpace(userName);

    // first get user namespace crd
    let terminal_meta_user = { ...terminal_meta };
    terminal_meta_user.namespace = namespace;

    try {
      // get crd
      const terminalUserDesc = await GetCRD(kc, terminal_meta_user, terminal_name);
      if (terminalUserDesc?.body?.status) {
        const terminalStatus = terminalUserDesc.body.status as TerminalStatus;
        if (terminalStatus.availableReplicas > 0) {
          // temporarily add domain scheme
          return jsonRes(res, { data: terminalStatus.domain || '' });
        } else {
          return jsonRes(res, { code: 201, data: terminalStatus.domain || '' });
        }
      }
    } catch (error) {}

    const terminal_yaml = generateTerminalTemplate({
      namespace: namespace,
      user_name: userName,
      terminal_name: terminal_name,
      token: userToken,
      currentTime: new Date().toISOString()
    });
    const result = await ApplyYaml(kc, terminal_yaml);
    jsonRes(res, { code: 201, data: result, message: '' });
  } catch (error) {
    console.log(error, '--------');
    jsonRes(res, { code: 500, error });
  }
}
