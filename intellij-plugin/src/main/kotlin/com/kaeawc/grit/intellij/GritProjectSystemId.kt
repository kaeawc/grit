package com.kaeawc.grit.intellij

import com.intellij.openapi.externalSystem.model.ProjectSystemId

/**
 * Unique system ID that identifies grit-managed projects within the IntelliJ
 * External System framework. All grit sync, resolve, and task operations are
 * routed through this ID.
 */
object GritProjectSystemId {
    @JvmField
    val GRIT = ProjectSystemId("GRIT")
}
