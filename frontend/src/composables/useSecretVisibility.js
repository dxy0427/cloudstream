import { reactive } from 'vue'

export function useSecretVisibility(fields) {
 const visible = reactive(Object.fromEntries(fields.map(field => [field, false])))

 const toggle = (field) => {
  if (!(field in visible)) return false
  visible[field] = !visible[field]
  return visible[field]
 }

 const reset = () => {
  for (const field of fields) visible[field] = false
 }

 return { visible, toggle, reset }
}
