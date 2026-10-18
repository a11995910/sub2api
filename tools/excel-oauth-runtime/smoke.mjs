// 在本地回环验证完整 Node → Python → curl_cffi 传输，不连接外部服务。
import assert from 'node:assert/strict'
import http from 'node:http'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
const root = process.env.EXCEL_RUNTIME_ROOT || '/opt/excel-oauth-runtime'
const { TlsFingerprintTransport } = await import(pathToFileURL(path.join(root, 'tosub2/src/tls-transport.mjs')))
const server = http.createServer((_req, res) => {
  res.setHeader('Content-Type', 'application/json')
  res.end('{"ok":true}')
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const transport = new TlsFingerprintTransport({ enabled: true, profile: 'chrome146' })
try {
  const response = await transport.request('GET', `http://127.0.0.1:${server.address().port}/`)
  assert.equal(response.status, 200)
  assert.deepEqual(await response.json(), { ok: true })
  console.log('Excel 自动授权运行时回环验证通过')
} finally {
  await transport.close()
  await new Promise(resolve => server.close(resolve))
}
