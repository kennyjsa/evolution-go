/**
 * Chatwoot API Service
 * Configuração do conector Chatwoot por instância.
 */

import apiClient from './client';

export interface ChatwootConfig {
  instanceId: string;
  enabled: boolean;
  url: string;
  accountId: string;
  /** O token da conta nunca volta do servidor; só se ele já está gravado. */
  accountTokenSet: boolean;
  inboxId: string;
  inboxIdentifier: string;
  markAsRead: boolean;
  syncGroups: boolean;
  /** Assina a mensagem com o nome de quem respondeu, como o Evolution faz. */
  signMsg: boolean;
  /** Separador entre nome e texto; vazio usa a quebra de linha. */
  signDelimiter: string;
  /** Caminho do webhook a configurar na inbox do Chatwoot. */
  webhookUrl: string;
}

export interface ChatwootConfigPayload {
  enabled: boolean;
  url: string;
  accountId: string;
  /** Vazio mantém o token já gravado, em vez de apagá-lo. */
  accountToken?: string;
  inboxId: string;
  inboxIdentifier: string;
  markAsRead: boolean;
  syncGroups: boolean;
  signMsg: boolean;
  signDelimiter: string;
}

/**
 * Busca a config da instância. Devolve null quando ainda não existe — é o caso
 * normal da primeira vez, e não um erro a mostrar para o operador.
 */
export const fetchChatwootConfig = async (
  instanceId: string,
): Promise<ChatwootConfig | null> => {
  try {
    const { data } = await apiClient.get<ChatwootConfig>(`/chatwoot/${instanceId}`);
    return data;
  } catch (error: unknown) {
    const status = (error as { response?: { status?: number } })?.response?.status;
    if (status === 404) return null;
    throw error;
  }
};

export const saveChatwootConfig = async (
  instanceId: string,
  payload: ChatwootConfigPayload,
): Promise<{ status: string; webhookUrl: string }> => {
  const { data } = await apiClient.put(`/chatwoot/${instanceId}`, payload);
  return data;
};

export const deleteChatwootConfig = async (instanceId: string): Promise<void> => {
  await apiClient.delete(`/chatwoot/${instanceId}`);
};
