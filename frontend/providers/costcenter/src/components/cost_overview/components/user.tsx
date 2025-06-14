import request from '@/service/request';
import useSessionStore from '@/stores/session';
import { displayMoney, formatMoney } from '@/utils/format';
import { Box, Button, Center, Flex, Image, Stack, SystemStyleObject, Text } from '@chakra-ui/react';
import { useQueryClient } from '@tanstack/react-query';

import CurrencySymbol from '@/components/CurrencySymbol';
import { RechargeContext } from '@/pages/cost_overview';
import useEnvStore from '@/stores/env';
import useOverviewStore from '@/stores/overview';
import jsyaml from 'js-yaml';
import { useTranslation } from 'next-i18next';
import { memo, useContext, useEffect, useMemo, useRef } from 'react';
import RechargeModal from '../../RechargeModal';
import TransferModal from '../../TransferModal';

export default memo(function UserCard({ balance , credits }: { balance: number , credits:number}) {
  const getSession = useSessionStore((state) => state.getSession);
  const transferEnabled = useEnvStore((state) => state.transferEnabled);
  const rechargeEnabled = useEnvStore((state) => state.rechargeEnabled);
  const rechargeSource = useOverviewStore((s) => s.rechargeSource);
  const setRechargeSource = useOverviewStore((s) => s.setRecharge);
  const { kubeconfig } = getSession();
  const k8s_username = useMemo(() => {
    try {
      let temp = jsyaml.load(kubeconfig);
      // @ts-ignore
      return temp?.users[0]?.name;
    } catch (error) {
      return '';
    }
  }, [kubeconfig]);
  const { t } = useTranslation();
  const session = useSessionStore().getSession();

  const rechargeRef = useContext(RechargeContext).rechargeRef;
  const transferRef = useRef<any>();
  const queryClient = useQueryClient();
  useEffect(() => {
    // 加锁
    // let timeout = -1 as any
    if (rechargeRef?.current && rechargeSource > 0) {
      setRechargeSource(rechargeSource - 1);
      rechargeRef?.current?.onOpen();
    }
  }, [rechargeRef?.current, rechargeSource]);
  const currency = useEnvStore((s) => s.currency);
  const stripePromise = useEnvStore((s) => s.stripePromise);
  const persudoPublic: SystemStyleObject = {
    content: '""',
    position: 'absolute',
    width: '400px',
    height: '400px',
    backgroundColor: 'white',
    borderRadius: '50%',
    left: '-37px',
    // right: '50%',
    opacity: 0.1
  };
  return (
    <>
      <Flex
        boxShadow={'0 4px #BCBFC3,0 8px #DFE2E6'}
        pt="13px"
        pb={'19px'}
        mb={'10px'}
        px="16px"
        position={'relative'}
        width="331px"
        bg={'grayModern.900'}
        borderRadius="8px"
        color="white"
        overflow="hidden"
        _before={{ ...persudoPublic, top: '40px' }}
        shrink={[1, 1, 1, 0]}
      >
        <Stack zIndex="2" flex={'1'} gap="0">
          <Flex alignItems={'center'}>
            <Text>{session?.user?.name}</Text>

            <Center ml={'auto'} width={'36px'} height={'36px'} bg={'white'} borderRadius="full">
              <Image
                width={session?.user?.avatar ? 'full' : '20px'}
                height={session?.user?.avatar ? 'full' : '20px'}
                objectFit={'cover'}
                borderRadius="full"
                src={session?.user?.avatar || ''}
                fallbackSrc={'/default-user.svg'}
                alt="user avator"
                draggable={'false'}
              />
            </Center>
          </Flex>
          <Box fontSize="12px" fontWeight="400" alignSelf={'center'} mt="6px !important">
            {t('Balance')}
          </Box>
          <Flex
            fontSize="24px"
            fontWeight="500"
            alignSelf={'center'}
            mt="3px !important"
            alignItems={'center'}
          >
            <CurrencySymbol color={'white'} boxSize="20px" type={currency} />
            <Text ml="6px">{displayMoney(formatMoney(balance))} + 🎁{displayMoney(formatMoney(credits))}</Text>
          </Flex>
          <Flex alignItems="center" alignSelf={'center'} gap="10px" mt={'20px !important'}>
            {transferEnabled && (
              <Button
                variant={'unstyled'}
                w="78px"
                h="32px"
                bg={'white'}
                color="black"
                onClick={(e) => {
                  e.preventDefault();
                  transferRef?.current!.onOpen();
                }}
              >
                {t('Transfer')}
              </Button>
            )}
            {rechargeEnabled && (
              <Button
                variant={'unstyled'}
                w="78px"
                h="32px"
                bg={'white'}
                color="black"
                onClick={(e) => {
                  e.preventDefault();
                  rechargeRef?.current!.onOpen();
                }}
              >
                {t('Charge')}
              </Button>
            )}
          </Flex>
        </Stack>
      </Flex>
      {
        <RechargeModal
          ref={rechargeRef}
          balance={balance}
          stripePromise={stripePromise}
          request={request}
          onPaySuccess={async () => {
            await new Promise((s) => setTimeout(s, 2000));
            await queryClient.invalidateQueries({ queryKey: ['billing'] });
            await queryClient.invalidateQueries({ queryKey: ['getAccount'] });
          }}
        />
      }
      {transferEnabled && (
        <TransferModal
          ref={transferRef}
          balance={balance}
          onTransferSuccess={async () => {
            await new Promise((s) => setTimeout(s, 2000));
            await queryClient.invalidateQueries({ queryKey: ['billing'] });
            await queryClient.invalidateQueries({ queryKey: ['getAccount'] });
          }}
          k8s_username={k8s_username}
        />
      )}
    </>
  );
});
