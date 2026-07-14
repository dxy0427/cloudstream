import { reactive, ref } from 'vue'

export function useSecretFields({
 fields,
 getValue,
 setValue,
 hasStoredValue = () => false,
 reveal,
 contextKey = () => ''
}) {
 const visible = reactive(Object.fromEntries(fields.map(field => [field, false])))
 const pending = ref(false)
 const pendingField = ref('')
 const revealBaselines = new Map()
 const editedFields = new Set()
 let generation = 0
 let activeRequest = null

 const markEdited = (field, value = getValue(field)) => {
  if (value === '' && revealBaselines.has(field)) {
   revealBaselines.delete(field)
   editedFields.delete(field)
   visible[field] = false
   return
  }
  if (revealBaselines.has(field) && Object.is(value, revealBaselines.get(field))) {
   editedFields.delete(field)
  } else {
   editedFields.add(field)
  }
 }

 const invalidateRequest = (field) => {
  if (activeRequest?.field !== field) return
  generation++
  activeRequest = null
  pending.value = false
  pendingField.value = ''
 }

 const clearField = (field) => {
  invalidateRequest(field)
  revealBaselines.delete(field)
  editedFields.delete(field)
  visible[field] = false
  setValue(field, '')
 }

 const reset = () => {
  generation++
  activeRequest = null
  pending.value = false
  pendingField.value = ''
  revealBaselines.clear()
  editedFields.clear()
  for (const field of fields) {
   visible[field] = false
   setValue(field, '')
  }
 }

 const valueForSubmit = (field) => {
  if (revealBaselines.has(field) && !editedFields.has(field)) return ''
  return getValue(field)
 }

 const hasExplicitValue = (field) => {
  const value = getValue(field)
  const hasValue = typeof value === 'string' ? Boolean(value.trim()) : value != null
  return hasValue && (!revealBaselines.has(field) || editedFields.has(field))
 }

 const toggle = async (field) => {
  if (visible[field]) {
   visible[field] = false
   return true
  }
  if (pending.value) return false

  if (!revealBaselines.has(field) && getValue(field) === '' && hasStoredValue(field)) {
   const request = {
    field,
    generation,
    contextKey: contextKey(field)
   }
    activeRequest = request
    editedFields.delete(field)
   pending.value = true
   pendingField.value = field
   try {
    const value = (await reveal(field)) ?? ''
    if (
     activeRequest !== request ||
     generation !== request.generation ||
     !Object.is(contextKey(field), request.contextKey)
    ) return false

    revealBaselines.set(field, value)
    editedFields.delete(field)
    setValue(field, value)
   } catch {
    return false
   } finally {
    if (activeRequest === request) {
     activeRequest = null
     pending.value = false
     pendingField.value = ''
    }
   }
  }

  visible[field] = true
  return true
 }

 return {
  visible,
  pending,
  pendingField,
  toggle,
  markEdited,
  clearField,
  reset,
  valueForSubmit,
  hasExplicitValue
 }
}
