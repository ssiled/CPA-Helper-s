const endpoint = process.env.CPA_TEST_ENDPOINT?.trim()
  || 'https://helper.silenceapi.site/v1/chat/completions'
const apiKey = process.env.CPA_TEST_KEY?.trim()
const concurrency = Number.parseInt(process.argv[2] ?? '20', 10)
const durationSeconds = Number.parseInt(process.argv[3] ?? '180', 10)

if (!apiKey) {
  throw new Error('CPA_TEST_KEY is required')
}
if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 200) {
  throw new Error('concurrency must be between 1 and 200')
}
if (!Number.isInteger(durationSeconds) || durationSeconds < 10 || durationSeconds > 1800) {
  throw new Error('durationSeconds must be between 10 and 1800')
}

const prompt = [
  'You are a senior SRE. Write a detailed incident report in Simplified Chinese about a',
  '2-core 2-GB server running Nginx, a Go model proxy, SQLite, and an account-pool',
  'plugin behind Cloudflare. The incident is intermittent HTTP 522 during concurrent',
  'model requests. Produce 800 to 1200 Chinese characters in six sections. Include at',
  'least eight executable checks and clearly distinguish ingress capacity, upstream',
  'account capacity, and memory held by long-lived streaming requests.',
].join(' ')

const payload = JSON.stringify({
  model: 'gpt-5.4-mini',
  messages: [{ role: 'user', content: prompt }],
  stream: true,
  stream_options: { include_usage: true },
  reasoning_effort: 'low',
  max_completion_tokens: 1600,
})

const startedAt = Date.now()
const deadline = startedAt + durationSeconds * 1000
const controllers = new Set()
const statuses = new Map()
const failureBodies = new Map()
const ttftSamples = []
const durationSamples = []

let active = 0
let started = 0
let completed = 0
let successful = 0
let failed = 0
let interrupted = 0
let aborted = 0
let totalBytes = 0
let totalTokens = 0
let stopping = false
let stopReason = null

function incrementStatus(status) {
  statuses.set(status, (statuses.get(status) ?? 0) + 1)
}

function percentile(values, quantile) {
  if (values.length === 0) return null
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.min(sorted.length - 1, Math.floor((sorted.length - 1) * quantile))]
}

function recordFailure(message) {
  const normalized = String(message || 'unknown error').slice(0, 500)
  failureBodies.set(normalized, (failureBodies.get(normalized) ?? 0) + 1)
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

function compactStats() {
  return {
    elapsed_seconds: Number(((Date.now() - startedAt) / 1000).toFixed(1)),
    active,
    started,
    completed,
    successful,
    failed,
    interrupted,
    aborted,
    statuses: Object.fromEntries([...statuses.entries()].sort(([a], [b]) => a - b)),
    ttft_ms_p50: percentile(ttftSamples, 0.5)?.toFixed(1) ?? null,
    ttft_ms_p95: percentile(ttftSamples, 0.95)?.toFixed(1) ?? null,
    duration_ms_p95: percentile(durationSamples, 0.95)?.toFixed(1) ?? null,
    total_tokens: totalTokens,
    total_bytes: totalBytes,
    stopping,
    stop_reason: stopReason,
  }
}

function stop(reason) {
  if (stopping) return
  stopping = true
  stopReason = reason
  for (const controller of controllers) {
    controller.abort(reason)
  }
}

process.once('SIGINT', () => stop('SIGINT'))
process.once('SIGTERM', () => stop('SIGTERM'))

function parseUsage(text) {
  let parsedTokens = 0
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) continue
    const data = trimmed.slice(5).trim()
    if (!data || data === '[DONE]') continue
    try {
      const event = JSON.parse(data)
      const value = Number(event?.usage?.total_tokens ?? 0)
      if (Number.isFinite(value) && value > parsedTokens) parsedTokens = value
    } catch {
      // Partial SSE frames are joined with the next network chunk by the caller.
    }
  }
  return parsedTokens
}

async function runRequest(workerId) {
  const controller = new AbortController()
  controllers.add(controller)
  active += 1
  started += 1
  const requestStartedAt = performance.now()
  let firstByteAt = null
  let pending = ''
  let requestTokens = 0
  let responseStatus = 0

  try {
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'Content-Type': 'application/json',
        'X-Load-Test-Worker': String(workerId),
      },
      body: payload,
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(180000)]),
    })

    responseStatus = response.status
    incrementStatus(responseStatus)
    if (!response.ok) {
      const body = (await response.text()).slice(0, 500)
      failed += 1
      recordFailure(body || `HTTP ${responseStatus}`)
      await delay(1000)
      return
    }

    const reader = response.body?.getReader()
    if (!reader) {
      failed += 1
      recordFailure('response body is not readable')
      return
    }

    const decoder = new TextDecoder()
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      if (firstByteAt === null) firstByteAt = performance.now()
      totalBytes += value.byteLength
      pending += decoder.decode(value, { stream: true })
      const boundary = pending.lastIndexOf('\n')
      if (boundary >= 0) {
        const complete = pending.slice(0, boundary + 1)
        pending = pending.slice(boundary + 1)
        requestTokens = Math.max(requestTokens, parseUsage(complete))
      }
    }
    pending += decoder.decode()
    requestTokens = Math.max(requestTokens, parseUsage(pending))

    const requestEndedAt = performance.now()
    if (firstByteAt !== null) ttftSamples.push(firstByteAt - requestStartedAt)
    durationSamples.push(requestEndedAt - requestStartedAt)
    totalTokens += requestTokens
    successful += 1
  } catch (error) {
    if (stopping && controller.signal.aborted) {
      aborted += 1
    } else {
      failed += 1
      if (responseStatus >= 200 && responseStatus < 300) {
        interrupted += 1
      } else if (responseStatus === 0) {
        incrementStatus(0)
      }
      recordFailure(`${error.name}: ${error.message}`)
    }
  } finally {
    controllers.delete(controller)
    active -= 1
    completed += 1
  }
}

async function worker(workerId) {
  while (!stopping && Date.now() < deadline) {
    await runRequest(workerId)
  }
}

const progressTimer = setInterval(() => {
  console.log(`PROGRESS ${JSON.stringify(compactStats())}`)
}, 15000)

await Promise.all(Array.from({ length: concurrency }, (_, index) => worker(index + 1)))
clearInterval(progressTimer)

console.log(
  `FINAL ${JSON.stringify({
    concurrency,
    planned_duration_seconds: durationSeconds,
    ...compactStats(),
    errors: [...failureBodies.entries()]
      .sort((left, right) => right[1] - left[1])
      .slice(0, 10)
      .map(([message, count]) => ({ count, message })),
  })}`,
)
