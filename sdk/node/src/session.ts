import type { AjahClient } from './client';
import type { AjahSessionChatOptions } from './types';
import OpenAI from 'openai';

export class AjahSession {
  readonly sessionId: string;
  private readonly client: AjahClient;
  private stepCounter = 0;

  constructor(client: AjahClient, sessionId: string) {
    this.client = client;
    this.sessionId = sessionId;
  }

  private nextStep(stepName?: string): string {
    this.stepCounter += 1;
    return stepName ?? `step-${this.stepCounter}`;
  }

  async chat(
    options: AjahSessionChatOptions,
  ): Promise<OpenAI.Chat.ChatCompletion> {
    const { stepName, ...rest } = options;
    return this.client.chat({
      ...rest,
      sessionId: this.sessionId,
      stepName: this.nextStep(stepName),
    });
  }

  get dashboardUrl(): string {
    return `${this.client.dashboardUrl}/sessions/${this.sessionId}`;
  }
}
