import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'

type Service = {
  id: string
  name: string
  baseUrl: string
  owner: string
  health: 'healthy' | 'degraded' | 'offline'
  lastChecked: string
}

type Route = {
  id: string
  serviceId: string
  name: string
  pathPrefix: string
  methods: string[]
  authMode: string
  rateLimitPerMinute: number
  canaryPercent: number
  enabled: boolean
  revision: number
  updatedAt: string
}

type ChangeRequest = {
  id: string
  routeId: string
  summary: string
  risk: string
  requestedBy: string
  status: string
  proposal: { rateLimitPerMinute?: number; canaryPercent?: number }
  approvedBy?: string
  createdAt: string
}

type AuditEvent = {
  sequence: number
  id: string
  action: string
  actor: string
  subject: string
  hash: string
  createdAt: string
}

type Dashboard = {
  services: Service[]
  routes: Route[]
  changes: ChangeRequest[]
  audit: AuditEvent[]
  auditChainValid: boolean
  mutationsEnabled: boolean
  mode: string
}

type Decision = {
  decision: 'allow' | 'deny'
  routeId?: string
  routeName?: string
  serviceId?: string
  upstream?: string
  trafficVariant?: string
  policyRevision?: number
  reasons: string[]
}

type LabInput = {
  method: string
  path: string
  identity: string
  requestsInLastMinute: number
  requestId: string
}

const scenarios: Array<{ label: string; input: LabInput }> = [
  {
    label: 'Allowed identity lookup',
    input: { method: 'GET', path: '/identity/users/42', identity: 'authenticated', requestsInLastMinute: 18, requestId: 'demo-identity-42' },
  },
  {
    label: 'Blocked deployment',
    input: { method: 'POST', path: '/deployments/release', identity: 'authenticated', requestsInLastMinute: 12, requestId: 'demo-deploy-denied' },
  },
  {
    label: 'Rate limit exceeded',
    input: { method: 'GET', path: '/audit/events', identity: 'authenticated', requestsInLastMinute: 94, requestId: 'demo-audit-throttle' },
  },
]

function App() {
  const [dashboard, setDashboard] = useState<Dashboard | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [lab, setLab] = useState<LabInput>(scenarios[0].input)
  const [decision, setDecision] = useState<Decision | null>(null)
  const [evaluating, setEvaluating] = useState(false)

  const loadDashboard = useCallback(async () => {
    try {
      setError('')
      const response = await fetch('/api/v1/dashboard', { headers: { Accept: 'application/json' } })
      if (!response.ok) throw new Error(`Dashboard request failed (${response.status})`)
      setDashboard((await response.json()) as Dashboard)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Dashboard data could not be loaded.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadDashboard()
  }, [loadDashboard])

  const stats = useMemo(() => {
    if (!dashboard) return { healthy: 0, routes: 0, protected: 0, pending: 0 }
    return {
      healthy: dashboard.services.filter((service) => service.health === 'healthy').length,
      routes: dashboard.routes.filter((route) => route.enabled).length,
      protected: dashboard.routes.filter((route) => route.authMode !== 'public').length,
      pending: dashboard.changes.filter((change) => change.status === 'pending').length,
    }
  }, [dashboard])

  async function evaluate(event: FormEvent) {
    event.preventDefault()
    setEvaluating(true)
    setError('')
    try {
      const response = await fetch('/api/v1/simulate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(lab),
      })
      const body = await response.json()
      if (!response.ok) throw new Error(body.error?.message ?? `Simulation failed (${response.status})`)
      setDecision(body as Decision)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The policy simulation failed.')
    } finally {
      setEvaluating(false)
    }
  }

  if (loading) {
    return <div className="center-state"><span className="pulse" />Loading the control plane…</div>
  }

  if (!dashboard) {
    return (
      <div className="center-state error-state">
        <span>Control plane unavailable</span>
        <small>{error}</small>
        <button type="button" onClick={() => void loadDashboard()}>Retry</button>
      </div>
    )
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark" aria-hidden="true"><span /></div>
          <div>
            <strong>HarborGate</strong>
            <span>API gateway control plane</span>
          </div>
        </div>
        <div className="topbar-meta">
          <span className="mode-pill"><i /> Simulation only</span>
          <span className="region">LOCAL · HG-01</span>
        </div>
      </header>

      <main>
        <section className="hero">
          <div>
            <p className="eyebrow">CONTROL PLANE / OVERVIEW</p>
            <h1>Every route has a reason.</h1>
            <p className="hero-copy">Inspect service health, test policy decisions and review proposed gateway changes without forwarding a single real request.</p>
          </div>
          <div className="integrity-card">
            <span className="integrity-icon">✓</span>
            <div>
              <small>AUDIT INTEGRITY</small>
              <strong>{dashboard.auditChainValid ? 'Chain verified' : 'Verification failed'}</strong>
            </div>
          </div>
        </section>

        {error && <div className="notice error-notice" role="alert">{error}</div>}

        <section className="stat-grid" aria-label="Gateway summary">
          <Metric label="HEALTHY SERVICES" value={`${stats.healthy}/${dashboard.services.length}`} detail="Observed upstreams" tone="green" />
          <Metric label="ENABLED ROUTES" value={String(stats.routes)} detail="Longest-prefix match" tone="blue" />
          <Metric label="PROTECTED ROUTES" value={String(stats.protected)} detail="Identity required" tone="violet" />
          <Metric label="PENDING CHANGES" value={String(stats.pending)} detail="Approval gated" tone="amber" />
        </section>

        <section className="workspace-grid">
          <div className="panel policy-lab">
            <PanelHeading kicker="REQUEST LAB" title="Evaluate a gateway decision" badge="Stateless" />
            <div className="scenario-row" aria-label="Example scenarios">
              {scenarios.map((scenario) => (
                <button key={scenario.label} type="button" onClick={() => { setLab(scenario.input); setDecision(null) }}>
                  {scenario.label}
                </button>
              ))}
            </div>
            <form onSubmit={evaluate}>
              <div className="form-row path-row">
                <label>
                  <span>Method</span>
                  <select value={lab.method} onChange={(event) => setLab({ ...lab, method: event.target.value })}>
                    {['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((method) => <option key={method}>{method}</option>)}
                  </select>
                </label>
                <label className="grow">
                  <span>Request path</span>
                  <input value={lab.path} onChange={(event) => setLab({ ...lab, path: event.target.value })} required />
                </label>
              </div>
              <div className="form-row">
                <label>
                  <span>Caller identity</span>
                  <select value={lab.identity} onChange={(event) => setLab({ ...lab, identity: event.target.value })}>
                    <option value="anonymous">Anonymous</option>
                    <option value="authenticated">Authenticated user</option>
                    <option value="service-token">Service token</option>
                  </select>
                </label>
                <label>
                  <span>Requests in last minute</span>
                  <input type="number" min="0" max="1000000" value={lab.requestsInLastMinute} onChange={(event) => setLab({ ...lab, requestsInLastMinute: Number(event.target.value) })} />
                </label>
              </div>
              <div className="lab-footer">
                <span>Request ID <code>{lab.requestId}</code></span>
                <button className="primary-button" type="submit" disabled={evaluating}>{evaluating ? 'Evaluating…' : 'Evaluate policy →'}</button>
              </div>
            </form>

            <div className={`decision-card ${decision?.decision ?? 'empty'}`}>
              {!decision ? (
                <div className="decision-empty"><span>◎</span>Select a scenario or enter a request to see the policy trace.</div>
              ) : (
                <>
                  <div className="decision-head">
                    <div>
                      <small>POLICY DECISION</small>
                      <strong>{decision.decision}</strong>
                    </div>
                    <span className="decision-mark">{decision.decision === 'allow' ? '✓' : '×'}</span>
                  </div>
                  <div className="decision-meta">
                    <span>Route <b>{decision.routeName ?? 'No match'}</b></span>
                    <span>Variant <b>{decision.trafficVariant ?? '—'}</b></span>
                    <span>Revision <b>{decision.policyRevision ? `r${decision.policyRevision}` : '—'}</b></span>
                  </div>
                  <ul>{decision.reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul>
                </>
              )}
            </div>
          </div>

          <div className="panel service-panel">
            <PanelHeading kicker="UPSTREAMS" title="Service health" badge={`${dashboard.services.length} registered`} />
            <div className="service-list">
              {dashboard.services.map((service) => (
                <article className="service-item" key={service.id}>
                  <div className={`health-orb ${service.health}`}><span /></div>
                  <div className="service-main">
                    <div><strong>{service.name}</strong><Status value={service.health} /></div>
                    <span>{service.owner}</span>
                    <code>{service.baseUrl.replace('https://', '')}</code>
                  </div>
                  <time>{formatAgo(service.lastChecked)}</time>
                </article>
              ))}
            </div>
            <div className="safe-boundary">
              <span>⌁</span>
              <div><strong>Safe boundary</strong><p>Health values are seeded observations. HarborGate never connects to these placeholder upstreams.</p></div>
            </div>
          </div>
        </section>

        <section className="panel route-panel">
          <PanelHeading kicker="POLICY INVENTORY" title="Active gateway routes" badge={`${dashboard.routes.length} policies`} />
          <div className="table-wrap">
            <table>
              <thead><tr><th>ROUTE</th><th>METHODS</th><th>AUTHENTICATION</th><th>RATE LIMIT</th><th>CANARY</th><th>UPSTREAM</th><th>REVISION</th></tr></thead>
              <tbody>
                {dashboard.routes.map((route) => {
                  const service = dashboard.services.find((item) => item.id === route.serviceId)
                  return (
                    <tr key={route.id}>
                      <td><strong>{route.pathPrefix}</strong><span>{route.name}</span></td>
                      <td><div className="method-list">{route.methods.map((method) => <code key={method}>{method}</code>)}</div></td>
                      <td><span className="auth-pill">{route.authMode}</span></td>
                      <td>{route.rateLimitPerMinute}<small> / min</small></td>
                      <td>{route.canaryPercent}%</td>
                      <td><span className={`dot ${service?.health}`} />{service?.name}</td>
                      <td><code>r{route.revision}</code></td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </section>

        <section className="bottom-grid">
          <div className="panel change-panel">
            <PanelHeading kicker="CHANGE CONTROL" title="Approval queue" badge={dashboard.mutationsEnabled ? 'Admin enabled' : 'Read only'} />
            {dashboard.changes.map((change) => (
              <article className="change-card" key={change.id}>
                <div className="change-top">
                  <span className={`risk ${change.risk}`}>{change.risk} risk</span>
                  <code>{change.id}</code>
                  <Status value={change.status} />
                </div>
                <h3>{change.summary}</h3>
                <div className="proposal-grid">
                  <div><span>Rate limit</span><strong>{change.proposal.rateLimitPerMinute}/min</strong></div>
                  <div><span>Canary cohort</span><strong>{change.proposal.canaryPercent}%</strong></div>
                  <div><span>Requested by</span><strong>{change.requestedBy}</strong></div>
                </div>
                <p className="approval-note">A different actor must approve this proposal before the configuration controller can apply it.</p>
              </article>
            ))}
          </div>

          <div className="panel audit-panel">
            <PanelHeading kicker="EVIDENCE" title="Recent audit events" badge={dashboard.auditChainValid ? 'SHA-256 verified' : 'Invalid chain'} />
            <div className="audit-list">
              {dashboard.audit.slice(0, 5).map((event) => (
                <article key={event.id}>
                  <div className="timeline-line"><span /></div>
                  <div>
                    <strong>{friendlyAction(event.action)}</strong>
                    <p><b>{event.actor}</b> · {event.subject}</p>
                    <code>{event.hash.slice(0, 12)}…</code>
                  </div>
                  <time>{formatAgo(event.createdAt)}</time>
                </article>
              ))}
            </div>
          </div>
        </section>
      </main>

      <footer>
        <span>HarborGate reference implementation</span>
        <span>LOCAL-FIRST · FAIL-CLOSED · NO LIVE PROXY</span>
      </footer>
    </div>
  )
}

function Metric({ label, value, detail, tone }: { label: string; value: string; detail: string; tone: string }) {
  return <article className={`metric ${tone}`}><span>{label}</span><strong>{value}</strong><small>{detail}</small><i /></article>
}

function PanelHeading({ kicker, title, badge }: { kicker: string; title: string; badge: string }) {
  return <div className="panel-heading"><div><span>{kicker}</span><h2>{title}</h2></div><small>{badge}</small></div>
}

function Status({ value }: { value: string }) {
  return <span className={`status ${value}`}>{value}</span>
}

function formatAgo(value: string) {
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  return `${Math.floor(seconds / 3600)}h ago`
}

function friendlyAction(value: string) {
  return value.split('.').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ')
}

export default App
