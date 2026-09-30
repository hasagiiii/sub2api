import priorityScheduling from './priorityScheduling'
import qualityOps from './qualityOps'
import accountOps from './accountOps'
import tokenGuard from './tokenGuard'
import pelicanTests from './pelicanTests'
import tokenGuardV2 from './tokenGuardV2'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import custom from './custom'
import support from './support'
import inbox from './inbox'
import organization from './organization'
import videoModels from './videoModels'
import materials from './materials'
import developerKeys from './developerKeys'
import { mergeLocaleMessages } from '../merge'

import requestTiming from './requestTiming'

import autoConfig from './autoConfig'

const upstream = {
  autoConfig,
  priorityScheduling,
  qualityOps,
  accountOps,
  tokenGuard,
  pelicanTests,
  tokenGuardV2,
  requestTiming,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...inbox,
  ...videoModels,
  ...materials,
}

export default mergeLocaleMessages(
  mergeLocaleMessages(mergeLocaleMessages(mergeLocaleMessages(upstream, custom), support), organization),
  developerKeys
)
