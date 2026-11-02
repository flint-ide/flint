// The feature modules (lib/features.ts): adding a feature is a directory here
// and one line in this list. Its Go half is listed in features.go at the
// repository root.
import type { FeatureModule } from '../lib/features'
import database from './database'
import extensions from './extensions'
import todo from './todo'
import update from './update'

export const FEATURES: readonly FeatureModule[] = [database, todo, update, extensions]
