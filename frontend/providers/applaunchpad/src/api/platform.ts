import { GET, POST } from '@/services/request';
import type { UserQuotaItemType, UserTask, userPriceType } from '@/types/user';
import { getUserSession } from '@/utils/user';
import { AuthCnamePrams } from './params';
import type { EnvResponse } from '@/types';

export const getResourcePrice = () => GET<userPriceType>('/api/platform/resourcePrice');

export const getInitData = () => GET<EnvResponse>('/api/platform/getInitData');

export const getUserQuota = () =>
  GET<{
    quota: UserQuotaItemType[];
  }>('/api/platform/getQuota');

export const postAuthCname = (data: AuthCnamePrams) => POST('/api/platform/authCname', data);

export const getUserTasks = () =>
  GET<{ needGuide: boolean; task: UserTask }>('/api/guide/getTasks', undefined, {
    headers: {
      Authorization: getUserSession()?.token
    }
  });

export const checkUserTask = () =>
  GET('/api/guide/checkTask', undefined, {
    headers: {
      Authorization: getUserSession()?.token
    }
  });

export const getPriceBonus = () =>
  GET<{ amount: number; gift: number }[]>('/api/guide/getBonus', undefined, {
    headers: {
      Authorization: getUserSession()?.token
    }
  });

export const checkPermission = (payload: { appName: string }) =>
  GET('/api/platform/checkPermission', payload);

export const checkReady = (appName: string) =>
  GET<{ url: string; ready: boolean; error?: string }[]>(`/api/checkReady?appName=${appName}`);

export const checkDomainResources = (data: { 
  domain: string; 
  networkingMode?: 'ingress' | 'istio' 
}) => POST<{
  exists: boolean;
  resourceType: string;
  resourceName: string;
  networkingMode: string;
}>('/api/platform/checkDomainResources', data);
