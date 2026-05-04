package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.model.settings.ExternalSystemExecutionSettings

/**
 * Settings passed to the project resolver and task manager when executing
 * grit operations. Will carry the grit executable path and project-specific
 * flags once those are implemented.
 */
class GritExecutionSettings : ExternalSystemExecutionSettings()
