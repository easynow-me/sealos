import { makeAPIClientByHeader } from '@/service/backend/region';
import { jsonRes } from '@/service/backend/response';
import type { NextApiRequest, NextApiResponse } from 'next';

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  try {
    const client = await makeAPIClientByHeader(req, res);
    if (!client) return;
    const response = await client.post('/payment/v1alpha1/credits/info', {});

    console.log(response.data);

    const data = response.data as {
      credits?: {
        UserUID: string;
        Balance: number;
        DeductionBalance: number;
        Credits: number;
        DeductionCredits: number;

        KYCDeductionCreditsDeductionBalance: number;
        KYCDeductionCreditsBalance: number;
        CurrentPlanCreditsBalance: number;
        CurrentPlanCreditsDeductionBalance: number;
      };
    };
    if (!data?.credits) return jsonRes(res, { code: 404, message: 'credit is not found' });
    return jsonRes<{ balance: number; deductionBalance: number,credits:number,deductionCredits:number }>(res, {
      data: {
        balance: data.credits.Balance,
        deductionBalance: data.credits.DeductionBalance,
        credits: data.credits.Credits,
        deductionCredits: data.credits.DeductionCredits
      }
    });
  } catch (error) {
    console.log(error);
    jsonRes(res, { code: 500, message: 'get credit error' });
  }
}
