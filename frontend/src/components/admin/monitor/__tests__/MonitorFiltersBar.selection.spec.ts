import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MonitorFiltersBar from '../MonitorFiltersBar.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

describe('monitor filter selection boundaries', () => {
  it('accepts every displayed provider and rejects values outside the declared choices', async () => {
    const wrapper = mount(MonitorFiltersBar, {
      props: { loading: false, search: '', provider: 'openai', enabled: '' },
      global: { stubs: { Select: true, Icon: true } },
    })
    const [provider] = wrapper.findAllComponents(Select)
    if (!provider) throw new Error('Missing provider filter')
    const choices = provider.props('options')
    for (const option of choices) {
      provider.vm.$emit('update:modelValue', option.value)
      await wrapper.vm.$nextTick()
    }
    expect(wrapper.emitted('update:provider')).toEqual(choices.map((option) => [option.value]))

    for (const value of [null, true, 1, 'undeclared-provider']) {
      provider.vm.$emit('update:modelValue', value)
      await wrapper.vm.$nextTick()
    }
    expect(wrapper.emitted('update:provider')).toHaveLength(choices.length)
  })

  it('preserves boolean-like string choices without coercing booleans', async () => {
    const wrapper = mount(MonitorFiltersBar, {
      props: { loading: false, search: '', provider: '', enabled: '' },
      global: { stubs: { Select: true, Icon: true } },
    })
    const [, enabled] = wrapper.findAllComponents(Select)
    if (!enabled) throw new Error('Missing enabled filter')
    for (const value of ['true', 'false', '']) {
      enabled.vm.$emit('update:modelValue', value)
      await wrapper.vm.$nextTick()
    }
    expect(wrapper.emitted('update:enabled')).toEqual([['true'], ['false'], ['']])
    for (const value of [true, false, null, 'invalid']) {
      enabled.vm.$emit('update:modelValue', value)
      await wrapper.vm.$nextTick()
    }
    expect(wrapper.emitted('update:enabled')).toHaveLength(3)
  })
})
