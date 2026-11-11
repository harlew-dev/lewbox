import { Logo } from '@harlew-dev/ui'

export function BrandLogo({
  href = '/',
  className,
}: {
  href?: string
  className?: string
}) {
  return <Logo href={href} name="Lewbox" className={className} />
}
