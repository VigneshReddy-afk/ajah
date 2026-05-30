declare global {
  interface Window {
    PulsarField: new (canvas: HTMLCanvasElement) => {
      setConfig(cfg: Record<string, unknown>): void
      on(cb: (evt: PulsarEvent) => void): void
      start(): void
      stop(): void
      burst(n: number): void
      emit(evt: PulsarEvent): void
    }
    PulsarMeta: {
      FEATURES: Array<{ name: string; w: number; lat: number; cost: number }>
      MODELS: string[]
      COLORS: Record<string, string>
      genEvent: () => PulsarEvent
    }
  }
}

export interface PulsarEvent {
  id: number
  t: number
  feature: string
  fconf: { name: string; w: number; lat: number; cost: number }
  model: string
  latency: number
  cost: number
  quality: number
  pii: boolean
  rag: string
  flare: boolean
}
