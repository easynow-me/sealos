import { passwordExistRequest, passwordLoginRequest, UserInfo } from '@/api/auth';
import useSessionStore from '@/stores/session';
import { Input, InputGroup, InputLeftElement, Text, Button, Stack } from '@chakra-ui/react';
import { useTranslation } from 'next-i18next';
import { useRouter } from 'next/router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { jwtDecode } from 'jwt-decode';
import { AccessTokenPayload } from '@/types/token';
import { getAdClickData, getInviterId, getUserSemData, sessionConfig } from '@/utils/sessionConfig';
import { I18nCommonKey } from '@/types/i18next';
import { SemData } from '@/types/sem';
import { ArrowRight } from 'lucide-react';

export default function usePassword({
  showError
}: {
  showError: (errorMessage: I18nCommonKey, duration?: number) => void;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const [isLoading, setIsLoading] = useState(false);

  const { register, handleSubmit, watch, trigger, getValues } = useForm<{
    username: string;
    password: string;
  }>();

  const setToken = useSessionStore((s) => s.setToken);
  const setSession = useSessionStore((s) => s.setSession);

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
            const adClickData = getAdClickData();
            const semData: SemData | null = getUserSemData();

            const result = await passwordExistRequest({ user: data.username });

            if (result?.code === 200) {
              // 用户存在，执行登录
              const loginResult = await passwordLoginRequest({
                user: data.username,
                password: data.password,
                inviterId,
                semData,
                adClickData
              });
              if (!!loginResult?.data) {
                await sessionConfig(loginResult.data);
                await router.replace('/');
              }
            } else if (result?.code === 201) {
              // 用户不存在，显示错误信息
              showError(t('common:invalid_username_or_password'));
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
    return <PasswordModal />;
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

  return {
    PasswordComponent,
    login,
    isLoading
  };
} 