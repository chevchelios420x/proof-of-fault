// Connection to the proof-of-fault service (local or on another computer).
// Every function of the Go App is called through POST /api/call/<Name>;
// live data arrives as server-sent events.

let onUnauth = () => {}
export function setUnauthHandler(fn) { onUnauth = fn }

async function request(url, opts = {}) {
  const res = await fetch(url, { credentials: 'same-origin', ...opts, headers: { 'X-POF': '1', ...(opts.headers || {}) } })
  if (res.status === 401) {
    onUnauth()
    throw 'Nicht angemeldet'
  }
  if (!res.ok) {
    let msg = res.statusText
    try { msg = (await res.json()).error || msg } catch {}
    throw msg
  }
  return res
}

async function call(name, ...args) {
  const res = await request('/api/call/' + name, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args),
  })
  return (await res.json()).result
}

const fn = (name) => (...args) => call(name, ...args)

export const StartMonitoring = fn('StartMonitoring')
export const StopMonitoring = fn('StopMonitoring')
export const GetStatus = fn('GetStatus')
export const ListSessions = fn('ListSessions')
export const GetSession = fn('GetSession')
export const SetHopWatched = fn('SetHopWatched')
export const SetHopZone = fn('SetHopZone')
export const SetHopName = fn('SetHopName')
export const GetVersion = fn('GetVersion')
export const GetLive = fn('GetLive')
export const GetRecent = fn('GetRecent')
export const SaveCustomPoint = fn('SaveCustomPoint')
export const DeleteCustomPoint = fn('DeleteCustomPoint')
export const AddSuggestedPoints = fn('AddSuggestedPoints')
export const GetSettings = fn('GetSettings')
export const SaveSettings = fn('SaveSettings')
export const DefaultSettings = fn('DefaultSettings')
export const DeleteSession = fn('DeleteSession')
export const SetSessionNote = fn('SetSessionNote')
export const GetDocsis = fn('GetDocsis')
export const TestFritz = fn('TestFritz')

function filename(res, fallback) {
  const m = /filename="?([^";]+)"?/.exec(res.headers.get('Content-Disposition') || '')
  return m ? decodeURIComponent(m[1]) : fallback
}

function save(blob, name) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(a.href), 10000)
}

// Export downloads a session ("html", "csv", "events" or "pof").
export async function Export(id, format) {
  const res = await request(`/api/export?id=${id}&format=${format}`)
  const name = filename(res, `proof-of-fault_${id}.${format}`)
  save(await res.blob(), name)
  return name
}

// FritzDebug downloads the FRITZ!Box debug ZIP.
export async function FritzDebug(url, user, password) {
  const res = await request('/api/fritzdebug', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, user, password }),
  })
  const name = filename(res, 'fritzbox-debug.zip')
  save(await res.blob(), name)
  return name
}

// ImportSession uploads an export file (.pof) and returns the new ID.
export async function ImportSession(file) {
  const res = await request('/api/import', { method: 'POST', body: file })
  return (await res.json()).result
}

// PeriodReportURL is the report over 24 h / 7 / 30 / 90 days.
export const PeriodReportURL = (download) => '/api/report/period' + (download ? '?download=1' : '')

export async function Login(key) {
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token: key }),
  })
  if (!res.ok) {
    let msg = 'Anmeldung fehlgeschlagen'
    try { msg = (await res.json()).error || msg } catch {}
    throw msg
  }
}

export async function Logout() {
  await fetch('/api/logout', { method: 'POST' })
}

export async function CheckAuth() {
  const res = await fetch('/api/auth', { credentials: 'same-origin' })
  return res.ok
}

// Events: one EventSource shared by all listeners; reconnects by itself.
const listeners = {}
let source = null
let onReconnect = () => {}
export function setReconnectHandler(fn) { onReconnect = fn }

function connect() {
  if (source) return
  source = new EventSource('/api/events')
  let opened = false
  source.onopen = () => {
    if (opened) onReconnect()
    opened = true
  }
  for (const ev of Object.keys(listeners)) attach(ev)
}

function attach(ev) {
  source.addEventListener(ev, (m) => {
    let data
    try { data = JSON.parse(m.data) } catch { return }
    for (const f of listeners[ev] || []) f(data)
  })
}

export function EventsOn(ev, f) {
  if (!listeners[ev]) {
    listeners[ev] = []
    if (source) attach(ev)
  }
  listeners[ev].push(f)
  connect()
}
