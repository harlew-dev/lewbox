import { LinkProvider, type LinkProps } from '@harlew-dev/ui'
import { forwardRef, type ReactNode } from 'react'
import { Link as RouterLink } from 'react-router'

const EXTERNAL = /^(https?:)?\/\//i

// The library's links go through react-router for client-side navigation.
// External URLs and bare fragments stay real anchors, as they must.
const AppLink = forwardRef<HTMLAnchorElement, LinkProps>(function AppLink(
  { href, reloadDocument, ...props },
  ref,
) {
  if (reloadDocument || href.startsWith('#') || EXTERNAL.test(href)) {
    return <a ref={ref} href={href} {...props} />
  }
  return <RouterLink ref={ref} to={href} {...props} />
})

export function AppLinkProvider({ children }: { children: ReactNode }) {
  return <LinkProvider component={AppLink}>{children}</LinkProvider>
}
