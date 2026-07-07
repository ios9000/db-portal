export type Env = 'prod' | 'test' | 'dev';

const LABELS: Record<Env, string> = {
  prod: 'PROD',
  test: 'TEST',
  dev: 'DEV',
};

/**
 * Environment badge: PROD red / TEST amber / DEV gray. The label text is
 * part of the encoding (guardrail D2: never color alone).
 */
export function EnvBadge({ env }: { env: Env }) {
  return <span className={`env-badge env-${env}`}>{LABELS[env]}</span>;
}
