import fs from 'node:fs'

// The run's temporary database, keyring and lock file, named by the config.
// Runs are independent, so a crash that skips this only leaves small files
// in the system temp directory; the next run takes over a dead port lock.
export default function teardown() {
  for (const name of ['LEWBOX_E2E_TMPDIR', 'LEWBOX_E2E_LOCK']) {
    const target = process.env[name]
    if (target) fs.rmSync(target, { recursive: true, force: true })
  }
}
