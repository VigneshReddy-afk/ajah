export interface AjahClientOptions {
  gatewayUrl?: string;
  apiKey: string;
  featureName?: string;
  userId?: string;
}

export interface AjahChatOptions {
  model: string;
  messages: Array<{
    role: 'user' | 'assistant' | 'system';
    content: string;
  }>;
  stepName?: string;
  sessionId?: string;
  [key: string]: unknown;
}

export interface AjahSessionChatOptions {
  model: string;
  messages: Array<{
    role: 'user' | 'assistant' | 'system';
    content: string;
  }>;
  stepName?: string;
  [key: string]: unknown;
}
