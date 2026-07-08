import type { Env } from './EnvBadge';

const TEXT: Record<Env, string> = {
  prod: 'PRODUCTION environment — actions here affect real users',
  test: 'TEST environment',
  dev: 'DEV environment',
};

/**
 * Full-width environment banner (guardrail layer 1, SPEC-015): every
 * single-env context (launch drawer, run detail) states its environment in
 * text AND color — never color alone (D2). Fleet screens stay banner-free.
 */
export function EnvBanner({ env }: { env: Env }) {
  return (
    <div className={`env-banner env-${env}`}>
      {env === 'prod' && <span aria-hidden="true">⚠ </span>}
      {TEXT[env]}
    </div>
  );
}
