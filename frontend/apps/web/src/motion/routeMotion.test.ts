import { describe, expect, it } from 'vitest'
import { detailParentPath, planRouteMotion, type SharedOrigin } from './routeMotion'

const origin: SharedOrigin = {
  x: 24,
  y: 180,
  width: 164,
  height: 205,
  radius: 18,
  shadow: 'none',
  scrollX: 0,
  scrollY: 720,
}

describe('route motion planning', () => {
  it('expands a clicked list card into its detail route', () => {
    expect(planRouteMotion(
      { name: 'edit-inspiration', path: '/inspirations/abc' },
      { name: 'gallery', path: '/inspirations' },
      origin,
      null,
    )).toEqual({ kind: 'shared-forward', origin })
  })

  it('returns a detail route to its remembered list position', () => {
    expect(planRouteMotion(
      { name: 'gallery', path: '/inspirations' },
      { name: 'edit-inspiration', path: '/inspirations/abc' },
      null,
      origin,
    )).toEqual({ kind: 'shared-back', origin })
  })

  it('uses a page transition when a detail route is opened directly', () => {
    expect(planRouteMotion(
      { name: 'edit-inspiration', path: '/inspirations/abc' },
      { name: 'workbench', path: '/workbench' },
      null,
      null,
    )).toEqual({ kind: 'page' })
  })

  it('does not animate same-path query updates', () => {
    expect(planRouteMotion(
      { name: 'projects', path: '/projects' },
      { name: 'projects', path: '/projects' },
      origin,
      origin,
    )).toEqual({ kind: 'none' })
  })

  it('knows the parent list of every shared detail route', () => {
    expect(detailParentPath({ name: 'portfolio-detail', path: '/portfolios/abc' })).toBe('/portfolios')
    expect(detailParentPath({ name: 'delivery-task', path: '/delivery/tasks/abc' })).toBe('/delivery')
    expect(detailParentPath({ name: 'gallery', path: '/inspirations' })).toBeUndefined()
  })
})
