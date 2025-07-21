import type { NextApiRequest, NextApiResponse } from 'next';
import { jsonRes } from '@/services/backend/response';

// Debug endpoint to check session status
export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  const { authorization } = req.headers;
  
  const sessionInfo = {
    hasAuthHeader: !!authorization,
    authHeaderLength: authorization?.length || 0,
    authHeaderPrefix: authorization?.substring(0, 50) || 'none',
    method: req.method,
    url: req.url,
    query: req.query
  };

  console.log('Session check:', sessionInfo);

  return jsonRes(res, {
    code: 200,
    data: sessionInfo
  });
}