import type { ReactNode } from 'react'
import { createContext, useContext, useEffect, useState } from 'react'

import { getUserEndpoint } from '../utils/endpoints'

interface AuthenticatedUser {
  username: string
  components: string[]
  team_slos?: string[]
}

interface AuthContextType {
  user: AuthenticatedUser | null
  loading: boolean
  isComponentAdmin: (componentSlug: string) => boolean
  isTeamSLOAdmin: (team: string) => boolean
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<AuthenticatedUser | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch(getUserEndpoint(), {
      credentials: 'include',
    })
      .then((response) => {
        if (response.ok) {
          return response.json()
        }
        return null
      })
      .then((userData) => {
        if (userData) {
          setUser(userData)
        }
      })
      .catch(() => {
        setUser(null)
      })
      .finally(() => {
        setLoading(false)
      })
  }, [])

  const isComponentAdmin = (componentSlug: string): boolean => {
    if (!user) {
      return false
    }
    return user.components.includes(componentSlug)
  }

  const isTeamSLOAdmin = (team: string): boolean => {
    if (!user) {
      return false
    }
    return (user.team_slos ?? []).includes(team)
  }

  return (
    <AuthContext.Provider value={{ user, loading, isComponentAdmin, isTeamSLOAdmin }}>
      {children}
    </AuthContext.Provider>
  )
}

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
