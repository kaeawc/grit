package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.settings.AbstractExternalSystemLocalSettings
import com.intellij.openapi.project.Project

/**
 * Machine-local settings for the grit external system (e.g., cached paths).
 */
class GritLocalSettings(project: Project) :
    AbstractExternalSystemLocalSettings<AbstractExternalSystemLocalSettings.State>(
        GritProjectSystemId.GRIT,
        project
    )
