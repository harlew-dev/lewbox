import { Logo } from '@harlew-dev/ui'
import { useInstance } from '../lib/instance'
import { routes } from '../lib/routes'

// The instance's name, as its operator set it.
export function BrandLogo({
  href = routes.home(),
  className,
}: {
  href?: string
  className?: string
}) {
  const { name } = useInstance()
  return <Logo href={href} name={name} className={className} />
}
