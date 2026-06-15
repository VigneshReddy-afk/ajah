# Ajah Node.js SDK

Node.js SDK for [Ajah](https://useajah.com) —
self-hosted LLM observability gateway.

## Installation

npm install ajah-sdk

## Quick Start

import { AjahClient } from 'ajah-sdk';

const client = new AjahClient({
  gatewayUrl: 'http://localhost:8080',
  apiKey: 'your-groq-key',
  featureName: 'my-app',
  userId: 'user-123',
});

const response = await client.chat({
  model: 'llama-3.3-70b-versatile',
  messages: [{ role: 'user', content: 'Hello' }],
});
console.log(response.choices[0].message.content);

## Session Tracking

const session = client.session();

const r1 = await session.chat({
  model: 'llama-3.3-70b-versatile',
  messages: [{ role: 'user', content: 'Plan a trip' }],
  stepName: 'step-1-planner',
});

const r2 = await session.chat({
  model: 'llama-3.3-70b-versatile',
  messages: [{ role: 'user', content: 'Book flights' }],
  stepName: 'step-2-booker',
});

console.log('Dashboard:', session.dashboardUrl);

## Self-Hosted

git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
docker-compose up -d

MIT License
