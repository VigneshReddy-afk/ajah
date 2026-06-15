import OpenAI from 'openai';
import { v4 as uuidv4 } from 'uuid';
import { AjahSession } from './session';
import type {
  AjahClientOptions,
  AjahChatOptions,
} from './types';

export class AjahClient {
  readonly gatewayUrl: string;
  readonly featureName: string;
  readonly userId: string;
  private readonly openai: OpenAI;

  constructor(options: AjahClientOptions) {
    this.gatewayUrl = (
      options.gatewayUrl ?? 'http://localhost:8080'
    ).replace(/\/$/, '');
    this.featureName = options.featureName ?? 'default';
    this.userId = options.userId ?? 'anonymous';
    this.openai = new OpenAI({
      baseURL: `${this.gatewayUrl}/v1`,
      apiKey: options.apiKey,
    });
  }

  private baseHeaders(
    sessionId?: string,
    stepName?: string,
  ): Record<string, string> {
    const headers: Record<string, string> = {
      'X-Feature-Name': this.featureName,
      'X-User-ID': this.userId,
    };
    if (sessionId) {
      headers['X-Session-ID'] = sessionId;
    }
    if (stepName) {
      headers['X-Agent-Step'] = stepName;
    }
    return headers;
  }

  async chat(options: AjahChatOptions): Promise<OpenAI.Chat.ChatCompletion> {
    const {
      model,
      messages,
      stepName,
      sessionId,
      ...rest
    } = options;

    return this.openai.chat.completions.create({
      model,
      messages,
      ...rest,
    } as OpenAI.Chat.ChatCompletionCreateParamsNonStreaming, {
      headers: this.baseHeaders(sessionId, stepName),
    });
  }

  session(sessionId?: string): AjahSession {
    return new AjahSession(
      this,
      sessionId ?? uuidv4(),
    );
  }

  get dashboardUrl(): string {
    return this.gatewayUrl.replace(':8080', ':3000');
  }
}
