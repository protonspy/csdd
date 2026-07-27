import { useEffect, useState } from 'react'
import { getTheme, subscribe, type Theme } from './theme'

/** Re-renders the caller whenever the theme changes. */
export function useTheme(): Theme {
  const [theme, setThemeState] = useState<Theme>(getTheme)
  useEffect(() => subscribe(setThemeState), [])
  return theme
}
