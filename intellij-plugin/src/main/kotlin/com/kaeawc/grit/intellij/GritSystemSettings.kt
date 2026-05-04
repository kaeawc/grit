package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.settings.AbstractExternalSystemSettings
import com.intellij.openapi.externalSystem.settings.ExternalSystemSettingsListener
import com.intellij.openapi.project.Project

/**
 * Application-level settings for the grit external system.
 */
class GritSystemSettings(project: Project) :
    AbstractExternalSystemSettings<GritSystemSettings, GritProjectSettings, GritSystemSettingsListener>(
        GritProjectSystemId.GRIT,
        project
    ) {

    override fun subscribe(listener: ExternalSystemSettingsListener<GritProjectSettings>, parentDisposable: com.intellij.openapi.Disposable) {
        // Will be wired when settings UI is implemented
    }

    override fun copyExtraSettingsFrom(settings: GritSystemSettings) {
        // No extra settings yet
    }

    override fun getLinkedProjectSettings(linkedProjectPath: String): GritProjectSettings? = null

    override fun checkSettings(old: GritProjectSettings, current: GritProjectSettings) {
        // No validation yet
    }

    override fun getState(): GritSettingsState = GritSettingsState()

    override fun loadState(state: GritSettingsState) {
        super.loadState(state)
    }
}

interface GritSystemSettingsListener : ExternalSystemSettingsListener<GritProjectSettings>
