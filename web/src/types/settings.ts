import type { SignupMode } from './auth'

export interface Settings {
  instance_name: string
  signup: SignupMode
  // A plan slug, or null for none.
  default_plan: string | null
  // The cluster slug new workspaces get the default plan in, or null.
  default_region: string | null
}

// Fields that are set change. default_plan "none" clears it; default_region
// "none" clears it.
export interface SettingsUpdate {
  instance_name?: string
  signup?: SignupMode
  default_plan?: string
  default_region?: string
}
