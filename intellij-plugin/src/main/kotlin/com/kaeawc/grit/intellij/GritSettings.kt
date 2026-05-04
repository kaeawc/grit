package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.settings.ExternalSystemSettingsState

/**
 * Per-project settings for grit external system integration.
 */
class GritProjectSettings(
    var gritExecutablePath: String = ""
)

/**
 * Serializable state for [GritSystemSettings].
 */
class GritSettingsState : ExternalSystemSettingsState()
