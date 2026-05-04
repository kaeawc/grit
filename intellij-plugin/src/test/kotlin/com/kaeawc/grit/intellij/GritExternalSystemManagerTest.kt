package com.kaeawc.grit.intellij

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Test

class GritExternalSystemManagerTest {

    @Test
    fun `manager reports GRIT system ID`() {
        val manager = GritExternalSystemManager()
        assertEquals(GritProjectSystemId.GRIT, manager.systemId)
    }

    @Test
    fun `manager provides project resolver class`() {
        val manager = GritExternalSystemManager()
        assertNotNull(manager.projectResolverClass)
        assertEquals(StubGritProjectResolver::class.java, manager.projectResolverClass)
    }

    @Test
    fun `manager provides task manager class`() {
        val manager = GritExternalSystemManager()
        assertNotNull(manager.taskManagerClass)
        assertEquals(StubGritTaskManager::class.java, manager.taskManagerClass)
    }

    @Test
    fun `manager provides external project descriptor`() {
        val manager = GritExternalSystemManager()
        assertNotNull(manager.externalProjectDescriptor)
    }
}
