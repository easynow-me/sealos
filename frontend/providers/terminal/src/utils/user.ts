import useSessionStore from '@/store/session';

// Edge
export const getUserKubeConfig = () => {
  let kubeConfig: string =
    process.env.NODE_ENV === 'development' ? process.env.NEXT_PUBLIC_MOCK_USER || '' : '';
  
  console.log('NODE_ENV:', process.env.NODE_ENV);
  console.log('Development mode kubeConfig:', kubeConfig ? '[HAS_CONFIG]' : '[EMPTY]');
  
  try {
    const session = useSessionStore.getState()?.session;
    console.log('Session exists:', !!session);
    console.log('Session kubeconfig exists:', !!session?.kubeconfig);
    
    if (!kubeConfig && session) {
      kubeConfig = session?.kubeconfig;
    }
  } catch (err) {
    console.error('Error getting session:', err);
  }
  
  console.log('Final kubeConfig length:', kubeConfig?.length || 0);
  return kubeConfig;
};
