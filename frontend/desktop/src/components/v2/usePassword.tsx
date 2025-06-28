import { passwordExistRequest, passwordLoginRequest, UserInfo } from '@/api/auth';
import useSessionStore from '@/stores/session';
import { Input, InputGroup, InputLeftElement, Text, Button, Stack, Flex, Box } from '@chakra-ui/react';
import { useTranslation } from 'next-i18next';
import { useRouter } from 'next/router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { jwtDecode } from 'jwt-decode';
import { AccessTokenPayload } from '@/types/token';
import { getBaiduId, getInviterId, getUserSemData, sessionConfig } from '@/utils/sessionConfig';
import { I18nCommonKey } from '@/types/i18next';
import { SemData } from '@/types/sem';
import { ArrowLeft, ArrowRight } from 'lucide-react';

export default function usePassword({
  showError
}: {
  showError: (errorMessage: I18nCommonKey, duration?: number) => void;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const [userExist, setUserExist] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  // 对于注册的用户，需要先验证密码 0 默认页面;1为验证密码页面
  const [pageState, setPageState] = useState(0);

  const setToken = useSessionStore((s) => s.setToken);
  const setSession = useSessionStore((s) => s.setSession);
  const { register, handleSubmit, watch, trigger, getValues } = useForm<{
    username: string;
    password: string;
    confimPassword: string;
  }>();

  const login = async () => {
    const deepSearch = (obj: any): I18nCommonKey => {
      if (!obj || typeof obj !== 'object') return 'submit_error';
      if (!!obj.message) {
        return obj.message;
      }
      return deepSearch(Object.values(obj)[0]);
    };

    await handleSubmit(
      async (data) => {
        if (data?.username && data?.password) {
          try {
            setIsLoading(true);
            const inviterId = getInviterId();
            const bdVid = getBaiduId();
            const semData: SemData | null = getUserSemData();

            const result = await passwordExistRequest({ user: data.username });

            if (result?.code === 200) {
              const result = await passwordLoginRequest({
                user: data.username,
                password: data.password,
                inviterId,
                semData,
                bdVid
              });
              if (!!result?.data) {
                await sessionConfig(result.data);
                await router.replace('/');
              }
              return;
            } else if (result?.code === 201) {
              setUserExist(!!result?.data?.exist);
              setPageState(1);
              if (!!data?.confimPassword) {
                if (data?.password !== data?.confimPassword) {
                  showError('password_mis_match');
                } else {
                  const regionResult = await passwordLoginRequest({
                    user: data.username,
                    password: data.password,
                    inviterId,
                    semData,
                    bdVid
                  });
                  if (!!regionResult?.data) {
                    setToken(regionResult.data.token);
                    const infoData = await UserInfo();
                    const payload = jwtDecode<AccessTokenPayload>(regionResult.data.token);
                    setSession({
                      token: regionResult.data.appToken, // fix cannot get appToken after login
                      user: {
                        k8s_username: payload.userCrName,
                        name: infoData.data?.info.nickname || '',
                        avatar: infoData.data?.info.avatarUri || '',
                        nsid: payload.workspaceId,
                        ns_uid: payload.workspaceUid,
                        userCrUid: payload.userCrUid,
                        userId: payload.userId,
                        userUid: payload.userUid,
                        realName: infoData.data?.info.realName || undefined,
                        enterpriseRealName: infoData.data?.info.enterpriseRealName || undefined,
                        userRestrictedLevel: infoData.data?.info.userRestrictedLevel || undefined
                      },
                      kubeconfig: regionResult.data.kubeconfig
                    });
                    await router.replace('/');
                  }
                }
              }
            }
          } catch (error: any) {
            console.log(error);
            showError(t('common:invalid_username_or_password'));
          } finally {
            setIsLoading(false);
          }
        }
      },
      (err) => {
        console.log(err);
        showError(deepSearch(err));
      }
    )();
  };

  const PasswordComponent = () => {
    if (pageState === 0) {
      return <PasswordModal />;
    } else {
      return <ConfirmPasswordModal />;
    }
  };

  const PasswordModal = () => {
    return (
      <Stack spacing={'16px'}>
        <InputGroup>
          <InputLeftElement color={'#71717A'} left={'12px'} h={'40px'}>
            <Text
              pl="10px"
              pr="8px"
              height={'20px'}
              borderRight={'1px'}
              fontSize={'14px'}
              borderColor={'#E4E4E7'}
            >
              👤
            </Text>
          </InputLeftElement>
          <Input
            height="40px"
            w="full"
            fontSize={'14px'}
            background="#FFFFFF"
            border="1px solid #E4E4E7"
            borderRadius="8px"
            placeholder={t('common:username') || ''}
            py="10px"
            pr={'12px'}
            pl={'60px'}
            color={'#71717A'}
            _autofill={{
              backgroundColor: 'transparent !important',
              backgroundImage: 'none !important'
            }}
            {...register('username', {
              pattern: {
                value: /^[a-zA-Z0-9_-]{3,16}$/,
                message: 'username tips'
              },
              required: true
            })}
          />
        </InputGroup>
        <InputGroup>
          <InputLeftElement color={'#71717A'} left={'12px'} h={'40px'}>
            <Text
              pl="10px"
              pr="8px"
              height={'20px'}
              borderRight={'1px'}
              fontSize={'14px'}
              borderColor={'#E4E4E7'}
            >
              🔒
            </Text>
          </InputLeftElement>
          <Input
            type="password"
            height="40px"
            w="full"
            fontSize={'14px'}
            background="#FFFFFF"
            border="1px solid #E4E4E7"
            borderRadius="8px"
            placeholder={t('common:password') || ''}
            py="10px"
            pr={'12px'}
            pl={'60px'}
            color={'#71717A'}
            _autofill={{
              backgroundColor: 'transparent !important',
              backgroundImage: 'none !important'
            }}
            {...register('password', {
              pattern: {
                value: /^(?=.*\S).{8,}$/,
                message: 'password tips'
              },
              required: true
            })}
          />
        </InputGroup>
        <Button
          variant={'solid'}
          px={'0'}
          borderRadius={'8px'}
          onClick={login}
          isLoading={isLoading}
          bgColor={'#0A0A0A'}
          rightIcon={<ArrowRight size={'14px'} />}
        >
          {t('v2:sign_in')}
        </Button>
      </Stack>
    );
  };

  const ConfirmPasswordModal = () => {
    return (
      <Stack spacing={'16px'}>
        <Flex p={'0'} alignItems={'center'} width="full" minH="42px" mb="14px" borderRadius="4px">
          <Box mr={'16px'}>
            <ArrowLeft
              color={'#71717A'}
              size={'20px'}
              cursor={'pointer'}
              onClick={() => setPageState(0)}
            />
          </Box>
          <Text color={'#71717A'}>{t('common:verify_password')}</Text>
        </Flex>
        <InputGroup>
          <InputLeftElement color={'#71717A'} left={'12px'} h={'40px'}>
            <Text
              pl="10px"
              pr="8px"
              height={'20px'}
              borderRight={'1px'}
              fontSize={'14px'}
              borderColor={'#E4E4E7'}
            >
              🔒
            </Text>
          </InputLeftElement>
          <Input
            type="password"
            height="40px"
            w="full"
            fontSize={'14px'}
            background="#FFFFFF"
            border="1px solid #E4E4E7"
            borderRadius="8px"
            placeholder={t('common:verify_password') || 'Verify password'}
            py="10px"
            pr={'12px'}
            pl={'60px'}
            color={'#71717A'}
            _autofill={{
              backgroundColor: 'transparent !important',
              backgroundImage: 'none !important'
            }}
            {...register('confimPassword', {
              pattern: {
                value: /^(?=.*\S).{8,}$/,
                message: 'password tips'
              },
              required: true
            })}
          />
        </InputGroup>
        <Button
          variant={'solid'}
          px={'0'}
          borderRadius={'8px'}
          onClick={login}
          isLoading={isLoading}
          bgColor={'#0A0A0A'}
          rightIcon={<ArrowRight size={'14px'} />}
        >
          {t('v2:sign_in')}
        </Button>
      </Stack>
    );
  };

  return {
    PasswordComponent,
    login,
    userExist,
    pageState,
    isLoading
  };
} 