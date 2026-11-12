import { Heading, Text } from '@harlew-dev/ui'
import {
  ChartBarIcon,
  KeyIcon,
  PlayCircleIcon,
  UserPlusIcon,
} from '@heroicons/react/24/outline'
import type { ComponentType } from 'react'
import { useNavigate } from 'react-router'
import { Carousel, type CarouselSlide } from '../../components/carousel'
import { OnboardingChrome } from './chrome'

// Placeholder artwork until the owner supplies real images: a soft gradient
// with a heroicon, in the 16/9 frame the real artwork will use.
function PlaceholderArt({ icon: Icon }: { icon: ComponentType<{ className?: string }> }) {
  return (
    <div className="flex aspect-video items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-indigo-100 via-white to-sky-100 ring-1 ring-zinc-950/5 dark:from-indigo-950 dark:via-zinc-900 dark:to-sky-950 dark:ring-white/10">
      <Icon className="size-20 text-indigo-400/80 dark:text-indigo-300/60" />
    </div>
  )
}

const slides: CarouselSlide[] = [
  {
    title: 'Create an API key',
    description:
      'Keys live in API keys. Create one for your workspace and copy it when it is shown — you won’t see it again.',
    image: <PlaceholderArt icon={KeyIcon} />,
  },
  {
    title: 'Run your first sandbox',
    description:
      'Use your key with the AerolVM or E2B SDK. Your apps talk to your region directly.',
    image: <PlaceholderArt icon={PlayCircleIcon} />,
  },
  {
    title: 'Watch usage and limits',
    description:
      'Usage shows what your sandboxes consume. Plan & Billing names your plan and the limits that come with it.',
    image: <PlaceholderArt icon={ChartBarIcon} />,
  },
  {
    title: 'Invite your team',
    description:
      'Add people to your workspace in Workspace settings → Members. Everyone shares its region, sandboxes and keys.',
    image: <PlaceholderArt icon={UserPlusIcon} />,
  },
]

// The short tour after a workspace is created.
export function GettingStartedPage() {
  const navigate = useNavigate()

  return (
    <OnboardingChrome width="max-w-2xl">
      <div className="space-y-2">
        <Heading>Getting started</Heading>
        <Text>A few things to try first in your new workspace.</Text>
      </div>
      <Carousel
        label="Getting started"
        slides={slides}
        onSkip={() => navigate('/', { replace: true })}
        onFinish={() => navigate('/', { replace: true })}
      />
    </OnboardingChrome>
  )
}
