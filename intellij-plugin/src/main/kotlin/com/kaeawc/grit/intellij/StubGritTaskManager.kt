package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.model.ExternalSystemException
import com.intellij.openapi.externalSystem.model.task.ExternalSystemTaskId
import com.intellij.openapi.externalSystem.model.task.ExternalSystemTaskNotificationListener
import com.intellij.openapi.externalSystem.task.ExternalSystemTaskManager

/**
 * Placeholder task manager. The real implementation will delegate task
 * execution to grit's CLI via the task bridge.
 */
class StubGritTaskManager : ExternalSystemTaskManager<GritExecutionSettings> {

    override fun executeTasks(
        id: ExternalSystemTaskId,
        taskNames: MutableList<String>,
        projectPath: String,
        settings: GritExecutionSettings?,
        jvmParametersSetup: String?,
        listener: ExternalSystemTaskNotificationListener
    ) {
        throw ExternalSystemException("Grit task manager is not yet implemented")
    }

    override fun cancelTask(id: ExternalSystemTaskId, listener: ExternalSystemTaskNotificationListener): Boolean = true
}
