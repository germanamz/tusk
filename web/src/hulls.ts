/**
 * hulls.ts — translucent convex-hull boundary overlay for the cluster lens.
 *
 * Owns the per-group THREE.Mesh objects on the graph's THREE.Scene. The overlay
 * is driven by the graph engine tick/stop hooks (throttled) so the hulls track
 * the live simulation positions without rebuilding every frame.
 *
 * The pure geometry helpers (groupMembers, hullEligible) are exported and
 * tested independently in hulls.test.ts without a WebGL context.
 */

import * as THREE from 'three'
import { ConvexGeometry } from 'three/examples/jsm/geometries/ConvexGeometry.js'
import type { Scene as ThreeScene } from 'three'

// HULL_OPACITY: low enough that several overlapping hulls do not wash out the
// scene, high enough to read as a distinct region. depthWrite=false and
// DoubleSide keep nodes/edges visible inside the hull from any camera angle.
const HULL_OPACITY = 0.14

/**
 * groupMembers groups nodes by their `group` field, excluding nodes with an
 * empty or absent group (they belong to no cluster and should not contribute
 * to any hull).
 *
 * Pure function — no THREE dependency; testable without a WebGL context.
 */
export function groupMembers(nodes: any[]): Map<string, any[]> {
  const out = new Map<string, any[]>()
  for (const node of nodes) {
    const grp: string = node.group ?? ''
    if (grp === '') continue
    const bucket = out.get(grp)
    if (bucket) {
      bucket.push(node)
    } else {
      out.set(grp, [node])
    }
  }
  return out
}

/**
 * hullEligible reports whether a group's member list is large enough for a
 * 3D convex hull. ConvexGeometry requires at least 4 non-coplanar points;
 * groups smaller than that are silently skipped.
 *
 * Pure function — no THREE dependency; testable without a WebGL context.
 */
export function hullEligible(members: any[]): boolean {
  return members.length >= 4
}

export interface HullOverlay {
  /** Recompute all hull meshes from nodes and the group→color map; no-op for groups with < 4 members. */
  update(nodes: any[], groupColors: Map<string, string>): void
  /** Toggle visibility without recomputing. */
  setEnabled(on: boolean): void
  /** Remove every mesh from the scene and dispose geometry/material. */
  clear(): void
}

export function createHullOverlay(scene: ThreeScene): HullOverlay {
  // Live mesh registry, keyed by group name.
  const meshes = new Map<string, THREE.Mesh>()
  let enabled = true

  function disposeMesh(mesh: THREE.Mesh): void {
    scene.remove(mesh)
    mesh.geometry.dispose()
    if (Array.isArray(mesh.material)) {
      mesh.material.forEach((m) => m.dispose())
    } else {
      ;(mesh.material as THREE.Material).dispose()
    }
  }

  function clearAll(): void {
    for (const mesh of meshes.values()) {
      disposeMesh(mesh)
    }
    meshes.clear()
  }

  return {
    update(nodes: any[], groupColors: Map<string, string>): void {
      // Always dispose the prior generation first to avoid mesh accumulation
      // across snapshots and position recomputes.
      clearAll()

      if (!enabled) return

      const byGroup = groupMembers(nodes)

      for (const [grp, members] of byGroup) {
        if (!hullEligible(members)) continue

        // Build the point cloud from live simulation coordinates. Nodes whose
        // x/y/z are undefined (not yet placed by d3-force) are skipped; if
        // fewer than 4 positioned members remain, the group is skipped entirely.
        const points: THREE.Vector3[] = []
        for (const nd of members) {
          if (nd.x == null || nd.y == null || nd.z == null) continue
          points.push(new THREE.Vector3(nd.x, nd.y, nd.z))
        }
        if (points.length < 4) continue

        const geometry = new ConvexGeometry(points)
        const color = groupColors.get(grp) ?? '#888888'
        const material = new THREE.MeshBasicMaterial({
          color: new THREE.Color(color),
          transparent: true,
          opacity: HULL_OPACITY,
          depthWrite: false,
          side: THREE.DoubleSide,
        })
        const mesh = new THREE.Mesh(geometry, material)
        scene.add(mesh)
        meshes.set(grp, mesh)
      }
    },

    setEnabled(on: boolean): void {
      enabled = on
      if (!on) {
        clearAll()
      }
    },

    clear(): void {
      clearAll()
    },
  }
}
