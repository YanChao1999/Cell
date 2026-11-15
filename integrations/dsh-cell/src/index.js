/** Lifecycle marker for dsh-plugin-cell. */
export const name = 'dsh-plugin-cell'

export function apply(ctx) {
  ctx?.logger?.info?.('[dsh-plugin-cell] loaded (tools via dsh-plugin-cell/tools)')
}
