import type { CodeTab } from '@harlew-dev/ui'
import type { Region } from './api'

// What a customer pastes to run a first sandbox in a region: the AerolVM
// environment variables and a short example per SDK. Without a token the
// key is a placeholder.
export function sandboxSnippets(region: Region, token = '<your API key>'): { env: string; tabs: CodeTab[] } {
  const url = region.endpoint_url.replace(/\/+$/, '')
  const firecracker = region.runtime === 'firecracker'
  const env = `export SB_API_URL=${url}\nexport SB_API_TOKEN=${token}`

  const tabs: CodeTab[] = [
    {
      label: 'TypeScript',
      language: 'ts',
      code: [
        '// npm i @aerol-ai/aerolvm-sdk',
        'import { MicroVM } from "@aerol-ai/aerolvm-sdk"',
        'const avm = new MicroVM() // reads SB_API_URL and SB_API_TOKEN',
        `const sb = await avm.create({ image: "ubuntu:22.04", cpu: 1, memoryMB: 1024${firecracker ? ', runtime: "firecracker"' : ''} })`,
        'const r = await sb.exec("echo hello")',
        'console.log(r.stdout)',
        'await sb.destroy()',
      ].join('\n'),
    },
    {
      label: 'Python',
      language: 'python',
      code: [
        '# pip install aerolvm-sdk',
        'from microvm import MicroVM',
        'avm = MicroVM()  # reads SB_API_URL and SB_API_TOKEN',
        `sb = avm.create({"image": "ubuntu:22.04", "cpu": 1, "memoryMB": 1024${firecracker ? ', "runtime": "firecracker"' : ''}})`,
        'print(sb.exec("echo hello")["stdout"])',
        'sb.destroy()',
      ].join('\n'),
    },
  ]

  if (region.runtime === 'docker') {
    tabs.push({
      label: 'E2B',
      language: 'ts',
      code: [
        '// npm i e2b@2.50.0',
        `// E2B_API_URL=${url}/e2b`,
        `// E2B_SANDBOX_URL=${url}/e2b/runtime`,
        `// E2B_API_KEY=${token}`,
        'import { Sandbox } from "e2b"',
        'const sbx = await Sandbox.create()',
        "console.log((await sbx.commands.run('echo hello')).stdout)",
        'await sbx.kill()',
      ].join('\n'),
    })
  }

  tabs.push({
    label: 'curl',
    language: 'sh',
    code: 'curl -H "Authorization: Bearer $SB_API_TOKEN" $SB_API_URL/v1/sandboxes',
  })

  return { env, tabs }
}
