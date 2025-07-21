import { IncomingHttpHeaders } from 'http';

export const authSession = async (header: IncomingHttpHeaders) => {
  if (!header) {
    console.error('authSession: No headers provided');
    return Promise.reject('unAuthorization: No headers');
  }
  
  const { authorization } = header;
  if (!authorization) {
    console.error('authSession: No authorization header');
    return Promise.reject('unAuthorization: No authorization header');
  }

  try {
    const kubeConfig = decodeURIComponent(authorization);
    if (!kubeConfig || kubeConfig === 'undefined' || kubeConfig === 'null') {
      console.error('authSession: Invalid kubeconfig after decode:', kubeConfig?.substring(0, 50));
      return Promise.reject('unAuthorization: Invalid kubeconfig');
    }
    return Promise.resolve(kubeConfig);
  } catch (err) {
    console.error('authSession: Failed to decode authorization:', err);
    return Promise.reject('unAuthorization: Decode failed');
  }
};

export const authAppToken = async (header: IncomingHttpHeaders) => {
  if (!header) return Promise.reject('unAuthorization');
  const { authorization } = header;
  if (!authorization) return Promise.reject('unAuthorization');

  try {
    return Promise.resolve(authorization);
  } catch (err) {
    return Promise.reject('unAuthorization');
  }
};
