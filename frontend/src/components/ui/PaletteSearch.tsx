import { Input, type InputProps } from '@flint-ide/ui'
import { Search } from '@flint-ide/ui/icons'

/**
 * The filter field on top of a palette (quick open, commands, symbols, branches…):
 * the kit's Input with a search icon, in the kit palette's search row. Takes the
 * input's props (value, onChange, onKeyDown, autoFocus…) as they are.
 */
export function PaletteSearch(props: InputProps) {
  return (
    <div className="fui-palette-search">
      <Input leading={<Search aria-hidden />} spellCheck={false} aria-label={props.placeholder} {...props} />
    </div>
  )
}
