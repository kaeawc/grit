package com.kaeawc.grit.intellij

import com.intellij.execution.ExecutionException
import com.intellij.openapi.externalSystem.ExternalSystemManager
import com.intellij.openapi.externalSystem.model.ProjectSystemId
import com.intellij.openapi.externalSystem.service.project.ExternalSystemProjectResolver
import com.intellij.openapi.externalSystem.task.ExternalSystemTaskManager
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Pair
import com.intellij.util.Function

/**
 * Entry point for grit's IntelliJ External System integration.
 *
 * Registers grit as a build system the IDE can delegate sync, resolve,
 * and task operations to. The IDE discovers this class via plugin.xml
 * and routes all operations for the [GritProjectSystemId.GRIT] system
 * through it.
 */
class GritExternalSystemManager :
    ExternalSystemManager<
        GritProjectSettings,
        GritSystemSettingsListener,
        GritSystemSettings,
        GritLocalSettings,
        GritExecutionSettings> {

    override fun getSystemId(): ProjectSystemId = GritProjectSystemId.GRIT

    override fun getSettingsProvider(): Function<Project, GritSystemSettings> =
        Function { project -> GritSystemSettings(project) }

    override fun getLocalSettingsProvider(): Function<Project, GritLocalSettings> =
        Function { project -> GritLocalSettings(project) }

    override fun getExecutionSettingsProvider(): Function<Pair<Project, String>, GritExecutionSettings> =
        Function { GritExecutionSettings() }

    override fun getProjectResolverClass(): Class<out ExternalSystemProjectResolver<GritExecutionSettings>> {
        @Suppress("UNCHECKED_CAST")
        return StubGritProjectResolver::class.java as Class<out ExternalSystemProjectResolver<GritExecutionSettings>>
    }

    override fun getTaskManagerClass(): Class<out ExternalSystemTaskManager<GritExecutionSettings>> {
        @Suppress("UNCHECKED_CAST")
        return StubGritTaskManager::class.java as Class<out ExternalSystemTaskManager<GritExecutionSettings>>
    }

    override fun getExternalProjectDescriptor(): FileChooserDescriptor =
        FileChooserDescriptor(true, false, false, false, false, false)
            .withFileFilter { it.name == "settings.grit.kts" || it.name == "build.grit.kts" }
}
