package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.model.DataNode
import com.intellij.openapi.externalSystem.model.ExternalSystemException
import com.intellij.openapi.externalSystem.model.project.ProjectData
import com.intellij.openapi.externalSystem.model.task.ExternalSystemTaskId
import com.intellij.openapi.externalSystem.model.task.ExternalSystemTaskNotificationListener
import com.intellij.openapi.externalSystem.service.project.ExternalSystemProjectResolver

/**
 * Placeholder project resolver. The real implementation will call grit's
 * CLI to produce the sync model and map it to IntelliJ DataNodes.
 */
class StubGritProjectResolver : ExternalSystemProjectResolver<GritExecutionSettings> {

    override fun resolveProjectInfo(
        id: ExternalSystemTaskId,
        projectPath: String,
        isPreviewMode: Boolean,
        settings: GritExecutionSettings?,
        listener: ExternalSystemTaskNotificationListener
    ): DataNode<ProjectData>? {
        throw ExternalSystemException("Grit project resolver is not yet implemented")
    }

    override fun cancelTask(taskId: ExternalSystemTaskId, listener: ExternalSystemTaskNotificationListener): Boolean = true
}
