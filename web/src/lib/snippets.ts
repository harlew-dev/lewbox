import type { CodeTab } from '@harlew-dev/ui'
import type { NewSandboxKey, Region } from './api'

// What a customer pastes to run a first sandbox in a region: the AerolVM
// environment variables and a short example per SDK. A new key's own
// variables are used as the API sent them; without one the key is a
// placeholder. The E2B example needs the key's e2b_env, which the API sends
// only where the region serves E2B.
export function sandboxSnippets(
  region: Region,
  key?: Pick<NewSandboxKey, 'env' | 'e2b_env'>,
): { env: string; tabs: CodeTab[] } {
  const firecracker = region.runtime === 'firecracker'
  const vars = key?.env ?? { SB_API_URL: region.endpoint_url.replace(/\/+$/, ''), SB_API_TOKEN: '<your API key>' }
  const env = `export SB_API_URL=${vars.SB_API_URL}\nexport SB_API_TOKEN=${vars.SB_API_TOKEN}`

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

  const e2b = key?.e2b_env
  if (e2b) {
    tabs.push({
      label: 'E2B',
      language: 'ts',
      code: [
        '// npm i e2b@2.50.0',
        `// E2B_API_URL=${e2b.E2B_API_URL}`,
        `// E2B_SANDBOX_URL=${e2b.E2B_SANDBOX_URL}`,
        `// E2B_API_KEY=${e2b.E2B_API_KEY}`,
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
